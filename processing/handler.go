package processing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/hibiken/asynq"

	"video-streaming-server/config"
	"video-streaming-server/queueing"
	"video-streaming-server/shared/logger"
	"video-streaming-server/statusbus"
	"video-streaming-server/storage"
	"video-streaming-server/types"
	"video-streaming-server/utils"
)

type Handler struct {
	DB *sql.DB
}

type sourceMetadata struct {
	duration  float64
	video     types.VideoCodec
	audio     types.AudioCodec
	container types.VideoContainer
	source    types.VideoSource
}

func (h *Handler) ProcessTask(ctx context.Context, task *asynq.Task) error {
	if task.Type() != queueing.TypeProcessVideo {
		return fmt.Errorf("unexpected task type %q", task.Type())
	}
	var payload queueing.ProcessVideoPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("decode video processing task: %w", err)
	}
	if payload.VideoID == "" {
		return errors.New("video processing task is missing video_id")
	}
	err := h.process(ctx, payload.VideoID)
	if err == nil {
		return nil
	}
	retryCount, retryOK := asynq.GetRetryCount(ctx)
	maxRetry, maxOK := asynq.GetMaxRetry(ctx)
	if retryOK && maxOK && retryCount >= maxRetry && ctx.Err() == nil {
		var ownerID, title string
		var metadata sourceMetadata
		if loadErr := h.DB.QueryRowContext(ctx, `SELECT user_id,title,COALESCE(duration_seconds,0),COALESCE(original_video_codec::text,'unknown'),COALESCE(original_audio_codec::text,'unknown'),COALESCE(original_container::text,'unknown'),video_source::text FROM videos WHERE video_id=$1 AND delete_flag=0 AND status=$2`, payload.VideoID, types.UploadedOnServer).Scan(&ownerID, &title, &metadata.duration, &metadata.video, &metadata.audio, &metadata.container, &metadata.source); loadErr == nil {
			if finishErr := h.finish(ctx, payload.VideoID, ownerID, title, types.ProcessingFailed, metadata, types.VideoCodecUnknown, types.AudioCodecUnknown, ""); finishErr != nil {
				return errors.Join(err, finishErr)
			}
			logger.Log.Error("video processing failed after retries", "video_id", payload.VideoID, "error", err)
			return nil
		}
	}
	return err
}

func (h *Handler) process(ctx context.Context, videoID string) error {
	ctx, finish, err := utils.BeginVideoProcessing(ctx, h.DB, videoID)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	defer finish()

	var title, key, ownerID string
	var source string
	var status types.VideoStatus
	err = h.DB.QueryRowContext(ctx, `SELECT title,source_file_key,user_id,status,video_source::text FROM videos WHERE video_id=$1 AND delete_flag=0`, videoID).Scan(&title, &key, &ownerID, &status, &source)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load video %s for processing: %w", videoID, err)
	}
	if status != types.UploadedOnServer {
		return nil
	}
	metadata := sourceMetadata{source: types.VideoSource(source)}
	log := logger.Log.With("video_id", videoID)
	client, err := storage.NewS3Client(ctx)
	if err != nil {
		return h.retryOrFail(ctx, videoID, ownerID, title, metadata, "", fmt.Errorf("create storage client: %w", err))
	}
	object, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(config.AppConfig.AppwriteBucketID), Key: aws.String(key)})
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return h.retryOrFail(ctx, videoID, ownerID, title, metadata, "", fmt.Errorf("download source video: %w", err))
	}
	defer object.Body.Close()

	workDir := filepath.Join(config.AppConfig.RootPath, "video", videoID)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return h.retryOrFail(ctx, videoID, ownerID, title, metadata, "", fmt.Errorf("create worker scratch directory: %w", err))
	}
	defer os.RemoveAll(workDir)
	sourcePath := filepath.Join(workDir, filepath.Base(key))
	sourceFile, err := os.Create(sourcePath)
	if err != nil {
		return h.retryOrFail(ctx, videoID, ownerID, title, metadata, "", fmt.Errorf("create source scratch file: %w", err))
	}
	if _, err := io.Copy(sourceFile, object.Body); err != nil {
		_ = sourceFile.Close()
		if ctx.Err() != nil {
			return nil
		}
		return h.retryOrFail(ctx, videoID, ownerID, title, metadata, "", fmt.Errorf("write downloaded source: %w", err))
	}
	if err := sourceFile.Close(); err != nil {
		return h.retryOrFail(ctx, videoID, ownerID, title, metadata, "", fmt.Errorf("close downloaded source: %w", err))
	}

	probe, err := utils.ProbeVideoMetadata(ctx, sourcePath)
	if err != nil {
		return h.retryOrFail(ctx, videoID, ownerID, title, metadata, "", fmt.Errorf("probe source video: %w", err))
	}
	metadata = normalizeMetadata(probe, filepath.Ext(key))
	metadata.source = types.VideoSource(source)
	if err := h.saveOriginalMetadata(ctx, videoID, metadata); err != nil {
		return h.retryOrFail(ctx, videoID, ownerID, title, metadata, "", fmt.Errorf("save source metadata: %w", err))
	}
	log.Info("source metadata extracted", "duration_seconds", metadata.duration, "video_codec", metadata.video, "audio_codec", metadata.audio, "container", metadata.container)

	output, err := utils.PostUploadProcessFile(ctx, utils.PostUploadProcessFileOptions{
		SourcePath: sourcePath,
		VideoID:    videoID,
		DB:         h.DB,
	})
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return nil
		}
		return h.retryOrFail(ctx, videoID, ownerID, title, metadata, output.ThumbnailURL, err)
	}
	finalVideo := types.VideoCodecH264
	if metadata.video == types.VideoCodecH264 {
		finalVideo = metadata.video
	}
	finalAudio := types.AudioCodecNone
	if metadata.audio != types.AudioCodecNone {
		finalAudio = types.AudioCodecAAC
	}
	return h.finish(ctx, videoID, ownerID, title, types.ProcessingCompleted, metadata, finalVideo, finalAudio, output.ThumbnailURL)
}

func (h *Handler) retryOrFail(ctx context.Context, videoID, ownerID, title string, metadata sourceMetadata, thumbnail string, cause error) error {
	retryCount, retryOK := asynq.GetRetryCount(ctx)
	maxRetry, maxOK := asynq.GetMaxRetry(ctx)
	if retryOK && maxOK && retryCount < maxRetry {
		return cause
	}
	if ctx.Err() != nil {
		return nil
	}
	if err := h.finish(ctx, videoID, ownerID, title, types.ProcessingFailed, metadata, types.VideoCodecUnknown, types.AudioCodecUnknown, thumbnail); err != nil {
		return errors.Join(cause, err)
	}
	logger.Log.Error("video processing failed after retries", "video_id", videoID, "error", cause)
	return nil
}

func (h *Handler) finish(ctx context.Context, videoID, ownerID, title string, status types.VideoStatus, metadata sourceMetadata, finalVideo types.VideoCodec, finalAudio types.AudioCodec, thumbnail string) error {
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin video processing result transaction: %w", err)
	}
	defer tx.Rollback()
	var finalVideoValue, finalAudioValue any
	var finalContainer any
	if status == types.ProcessingCompleted {
		finalVideoValue, finalAudioValue, finalContainer = string(finalVideo), string(finalAudio), string(types.VideoContainerHLS)
	} else {
		finalVideoValue, finalAudioValue, finalContainer = nil, nil, nil
	}
	result, err := tx.ExecContext(ctx, `UPDATE videos SET status=$1,upload_end_time=CASE WHEN $2 THEN NOW() ELSE upload_end_time END,duration_seconds=COALESCE(NULLIF($3::numeric,0::numeric),duration_seconds),original_video_codec=COALESCE($4,original_video_codec),original_audio_codec=COALESCE($5,original_audio_codec),original_container=COALESCE($6,original_container),final_video_codec=$7,final_audio_codec=$8,final_container=$9 WHERE video_id=$10 AND user_id=$11 AND delete_flag=0 AND status=$12`, status, status == types.ProcessingCompleted, metadata.duration, nullableVideoCodec(metadata.video), nullableAudioCodec(metadata.audio), nullableContainer(metadata.container), finalVideoValue, finalAudioValue, finalContainer, videoID, ownerID, types.UploadedOnServer)
	if err != nil {
		return fmt.Errorf("update video processing status: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected video rows: %w", err)
	}
	if affected == 0 {
		return nil
	}
	response := types.VideoResponseType{
		ID: videoID, Title: title, Thumbnail: thumbnail, Status: status,
		OriginalVideoCodec: metadata.video,
		OriginalAudioCodec: metadata.audio, OriginalContainer: metadata.container,
		VideoSource: metadata.source,
	}
	if metadata.duration > 0 {
		response.DurationSeconds = floatPointer(metadata.duration)
	}
	if status == types.ProcessingCompleted {
		response.FinalVideoCodec = finalVideo
		response.FinalAudioCodec = finalAudio
		response.FinalContainer = types.VideoContainerHLS
	}
	if _, err := statusbus.InsertVideoStatusOutboxEvent(ctx, tx, types.UserID(ownerID), response); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit video processing result: %w", err)
	}
	return nil
}

func (h *Handler) saveOriginalMetadata(ctx context.Context, videoID string, metadata sourceMetadata) error {
	_, err := h.DB.ExecContext(ctx, `UPDATE videos SET duration_seconds=$1,original_video_codec=$2,original_audio_codec=$3,original_container=$4 WHERE video_id=$5 AND delete_flag=0 AND status=$6`, metadata.duration, nullableVideoCodec(metadata.video), nullableAudioCodec(metadata.audio), nullableContainer(metadata.container), videoID, types.UploadedOnServer)
	return err
}

func normalizeMetadata(probe *types.FFProbeOutput, extension string) sourceMetadata {
	metadata := sourceMetadata{video: types.VideoCodecNone, audio: types.AudioCodecNone, container: normalizeContainer(probe.Format.FormatName, extension)}
	if duration, err := strconv.ParseFloat(probe.Format.Duration, 64); err == nil && !math.IsNaN(duration) && !math.IsInf(duration, 0) && duration >= 0 {
		metadata.duration = duration
	}
	for _, stream := range probe.Streams {
		switch stream.CodecType {
		case "video":
			if metadata.video == types.VideoCodecNone {
				metadata.video = normalizeVideoCodec(stream.CodecName)
			}
		case "audio":
			if metadata.audio == types.AudioCodecNone {
				metadata.audio = normalizeAudioCodec(stream.CodecName)
			}
		}
	}
	return metadata
}

func normalizeVideoCodec(codec string) types.VideoCodec {
	switch strings.ToLower(codec) {
	case "h264", "avc":
		return types.VideoCodecH264
	case "hevc", "h265":
		return types.VideoCodecHEVC
	case "vp9":
		return types.VideoCodecVP9
	case "av1":
		return types.VideoCodecAV1
	case "mpeg4":
		return types.VideoCodecMPEG4
	case "mpeg2video":
		return types.VideoCodecMPEG2
	case "":
		return types.VideoCodecUnknown
	default:
		return types.VideoCodecOther
	}
}

func normalizeAudioCodec(codec string) types.AudioCodec {
	switch strings.ToLower(codec) {
	case "aac":
		return types.AudioCodecAAC
	case "mp3":
		return types.AudioCodecMP3
	case "opus":
		return types.AudioCodecOpus
	case "vorbis":
		return types.AudioCodecVorbis
	case "ac3":
		return types.AudioCodecAC3
	case "eac3":
		return types.AudioCodecEAC3
	case "pcm_s16le", "pcm_s24le", "pcm_s32le", "pcm_f32le", "pcm_f64le":
		return types.AudioCodecPCM
	case "":
		return types.AudioCodecUnknown
	default:
		return types.AudioCodecOther
	}
}

func normalizeContainer(formatName string, extension string) types.VideoContainer {
	if strings.Contains(strings.ToLower(formatName), "mov") {
		if strings.EqualFold(extension, ".mov") {
			return types.VideoContainerMOV
		}
		if strings.EqualFold(extension, ".mp4") {
			return types.VideoContainerMP4
		}
	}
	for _, name := range strings.Split(strings.ToLower(formatName), ",") {
		switch strings.TrimSpace(name) {
		case "mp4", "m4a", "3gp", "3g2", "mj2":
			return types.VideoContainerMP4
		case "matroska":
			return types.VideoContainerMatroska
		case "mov":
			return types.VideoContainerMOV
		case "webm":
			return types.VideoContainerWebM
		case "mpegts":
			return types.VideoContainerMPEGTS
		}
	}
	if formatName == "" {
		return types.VideoContainerUnknown
	}
	return types.VideoContainerOther
}

func nullableVideoCodec(value types.VideoCodec) any {
	if value == "" {
		return nil
	}
	return string(value)
}

func nullableAudioCodec(value types.AudioCodec) any {
	if value == "" {
		return nil
	}
	return string(value)
}

func nullableContainer(value types.VideoContainer) any {
	if value == "" {
		return nil
	}
	return string(value)
}

func floatPointer(value float64) *float64 { return &value }
