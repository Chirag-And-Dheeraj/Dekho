package utils

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"video-streaming-server/config"
	"video-streaming-server/database"
	"video-streaming-server/repositories"
	"video-streaming-server/shared"
	"video-streaming-server/shared/logger"
	"video-streaming-server/storage"
	"video-streaming-server/storagekeys"
	"video-streaming-server/types"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/golang-jwt/jwt/v5"
)

var videoProcessing *slog.Logger

func extractThumbnail(videoPath string, fileName string) (string, error) {
	thumbnailPath := filepath.Join(config.AppConfig.RootPath, storagekeys.ThumbnailsFolder, fileName+".png")
	if err := os.MkdirAll(filepath.Dir(thumbnailPath), os.ModePerm); err != nil {
		return "", fmt.Errorf("error creating thumbnail directory: %w", err)
	}
	cmd := exec.Command("ffmpeg", "-y", "-i", videoPath, "-frames:v", "1", thumbnailPath)

	output, err := cmd.CombinedOutput()

	if err != nil {
		return "", fmt.Errorf("error extracting thumbnail: %w, output: %s", err, string(output))
	}

	return thumbnailPath, nil
}

func uploadThumbnailToStorage(videoID string, db *sql.DB) (string, error) {
	thumbnailPath := filepath.Join(config.AppConfig.RootPath, storagekeys.ThumbnailsFolder, videoID+".png")
	file, err := os.Open(thumbnailPath)
	if err != nil {
		return "", fmt.Errorf("open thumbnail: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("stat thumbnail: %w", err)
	}
	key := storagekeys.Thumbnail(videoID)
	if err := storage.PutObject(context.Background(), config.AppConfig.AppwriteBucketID, key, "image/png", file, info.Size()); err != nil {
		return "", err
	}
	thumbnailURL := "/video/" + videoID + "/thumbnail"
	if _, err := db.Exec(`UPDATE videos SET thumbnail=$1 WHERE video_id=$2`, thumbnailURL, videoID); err != nil {
		return "", fmt.Errorf("update thumbnail URL in database: %w", err)
	}
	if err := os.Remove(thumbnailPath); err != nil {
		return "", fmt.Errorf("remove local thumbnail: %w", err)
	}
	videoProcessing.Info("thumbnail uploaded to S3 storage", "object_key", key)
	return thumbnailURL, nil
}

func breakFile(videoPath string, videoID string) error {
	videoProcessing.Debug("Breaking file into HLS chunks", "video_path", videoPath)
	hlsDirectory := filepath.Join(config.AppConfig.RootPath, storagekeys.HLSChunksFolder, videoID)
	manifestPath := filepath.Join(config.AppConfig.RootPath, storagekeys.ManifestsFolder, videoID+".m3u8")
	if err := os.MkdirAll(hlsDirectory, os.ModePerm); err != nil {
		return fmt.Errorf("create HLS chunk directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(manifestPath), os.ModePerm); err != nil {
		return fmt.Errorf("create manifest directory: %w", err)
	}
	metaData, err := extractMetaData(videoPath)
	if err != nil {
		return fmt.Errorf("extract video metadata: %w", err)
	}
	videoCodec, audioCodec := "", ""
	for _, stream := range metaData.Streams {
		switch stream.CodecType {
		case "video":
			videoCodec = stream.CodecName
		case "audio":
			audioCodec = stream.CodecName
		}
	}
	videoCodecAction, audioCodecAction := "copy", "copy"
	if videoCodec != "h264" {
		videoProcessing.Info("Converting video codec", "old_video_codec", videoCodec, "new_video_codec", "h264")
		videoCodecAction = "libx264"
	}
	if audioCodec != "aac" {
		videoProcessing.Info("Converting audio codec", "old_audio_codec", audioCodec, "new_audio_codec", "aac")
		audioCodecAction = "aac"
	}
	segmentPattern := filepath.Join(hlsDirectory, videoID+"_segment_no_%d.ts")
	cmd := exec.Command("ffmpeg", "-y", "-i", videoPath, "-c:v", videoCodecAction, "-preset", "veryfast", "-c:a", audioCodecAction, "-map", "0", "-f", "segment", "-segment_time", "4", "-segment_format", "mpegts", "-segment_list", manifestPath, "-segment_list_type", "m3u8", segmentPattern)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("break file into HLS chunks: %w, output: %s", err, string(output))
	}
	return nil
}

func uploadHLSAssetsToStorage(videoID string) error {
	client, err := storage.NewS3Client(context.Background())
	if err != nil {
		return fmt.Errorf("create S3 client: %w", err)
	}
	hlsDirectory := filepath.Join(config.AppConfig.RootPath, storagekeys.HLSChunksFolder, videoID)
	files, err := os.ReadDir(hlsDirectory)
	if err != nil {
		return fmt.Errorf("read HLS chunk directory: %w", err)
	}
	for _, entry := range files {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".ts" {
			continue
		}
		filePath := filepath.Join(hlsDirectory, entry.Name())
		file, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("open HLS chunk: %w", err)
		}
		info, err := file.Stat()
		if err == nil {
			_, err = client.PutObject(context.Background(), &s3.PutObjectInput{
				Bucket: aws.String(config.AppConfig.AppwriteBucketID), Key: aws.String(storagekeys.HLSChunk(videoID, entry.Name())),
				ContentType: aws.String("video/MP2T"), Body: file, ContentLength: aws.Int64(info.Size()),
			})
		}
		_ = file.Close()
		if err != nil {
			return fmt.Errorf("upload HLS chunk %q: %w", entry.Name(), err)
		}
		if err := os.Remove(filePath); err != nil {
			return fmt.Errorf("remove local HLS chunk: %w", err)
		}
	}
	if err := os.Remove(hlsDirectory); err != nil {
		return fmt.Errorf("remove local HLS directory: %w", err)
	}
	manifestPath := filepath.Join(config.AppConfig.RootPath, storagekeys.ManifestsFolder, videoID+".m3u8")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("open HLS manifest: %w", err)
	}
	manifestLines := strings.Split(string(manifest), "\n")
	for index, line := range manifestLines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			manifestLines[index] = "/video/" + videoID + "/stream/" + filepath.Base(trimmed)
		}
	}
	manifest = []byte(strings.Join(manifestLines, "\n"))
	_, err = client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(config.AppConfig.AppwriteBucketID), Key: aws.String(storagekeys.Manifest(videoID)),
		ContentType: aws.String("application/vnd.apple.mpegurl"), Body: strings.NewReader(string(manifest)), ContentLength: aws.Int64(int64(len(manifest))),
	})
	if err != nil {
		return fmt.Errorf("upload HLS manifest: %w", err)
	}
	if err := os.Remove(manifestPath); err != nil {
		return fmt.Errorf("remove local HLS manifest: %w", err)
	}
	videoProcessing.Info("HLS chunks and manifest uploaded to S3 storage", "video_id", videoID)
	return nil
}

func closeAndRemoveTmpfile(tmpFile *os.File) (errClose, errRemove error) {
	err := tmpFile.Close()

	if err != nil {
		return fmt.Errorf("error closing temporary file: %w", err), nil
	}

	err = os.Remove(tmpFile.Name())

	if err != nil {
		return nil, fmt.Errorf("error removing temporary file: %w", err)
	}
	return nil, nil
}

func PostUploadProcessFile(serverFileName string, fileName string, videoTitle string, tmpFile *os.File, db *sql.DB, userID types.UserID) {
	videoProcessing = logger.Log.With("video_id", fileName)

	videoProcessing.Info("processing video")

	extractedThumbnail, err := extractThumbnail(("./video/" + serverFileName), fileName)
	thumbnailURL := ""

	if err != nil {
		videoProcessing.Error("error extracting thumbnail for video", "error", err)
	} else {
		videoProcessing.Debug("extracted thumbnail for video", "thumbnail", extractedThumbnail)
		thumbnailURL, err = uploadThumbnailToStorage(fileName, db)
		if err != nil {
			videoProcessing.Error("error uploading thumbnail to storage", "error", err)
		}
		videoProcessing.Info("uploaded thumbnail to storage", "thumbnail_url", thumbnailURL)
	}

	err = breakFile(("./video/" + serverFileName), fileName)

	if err != nil {
		videoProcessing.Error("error breaking file into segments", "error", err)
		if err := UpdateVideoStatus(db, fileName, types.ProcessingFailed); err != nil {
			videoProcessing.Error("error updating upload status for video in DB", "error", err)
		}
		shared.SendEventToUser(userID, "video_status", types.VideoResponseType{
			ID:     fileName,
			Title:  videoTitle,
			Status: types.ProcessingFailed,
		})
		return
	}

	videoProcessing.Info("broken file into segments")

	errClose, errRemove := closeAndRemoveTmpfile(tmpFile)
	if errClose != nil {
		videoProcessing.Error("error closing temporary file", "error", err)
		if err := UpdateVideoStatus(db, fileName, types.ProcessingFailed); err != nil {
			log.Printf("Error updating upload status for video %s in DB: %v", fileName, err)
		}
		shared.SendEventToUser(userID, "video_status", types.VideoResponseType{
			ID:     fileName,
			Title:  videoTitle,
			Status: types.ProcessingFailed,
		})
		return
	}

	if errRemove != nil {
		videoProcessing.Warn("error removing temporary file", "error", err)
	}

	err = uploadHLSAssetsToStorage(fileName)
	if err != nil {
		videoProcessing.Error("error uploading HLS assets to storage", "error", err)
		if err := UpdateVideoStatus(db, fileName, types.ProcessingFailed); err != nil {
			videoProcessing.Error("error updating upload status for video in DB", "error", err)
		}

		shared.SendEventToUser(userID, "video_status", types.VideoResponseType{
			ID:     fileName,
			Title:  videoTitle,
			Status: types.ProcessingFailed,
		})
		return
	}

	videoProcessing.Info("uploaded HLS assets to storage")
	if err := UpdateVideoStatus(db, fileName, types.ProcessingCompleted); err != nil {
		videoProcessing.Error("error updating upload status for video in DB", "error", err)
	}
	shared.SendEventToUser(userID, "video_status", types.VideoResponseType{
		ID:        fileName,
		Title:     videoTitle,
		Status:    types.ProcessingCompleted,
		Thumbnail: thumbnailURL,
	})
}

func GetStorageObjectBytes(key string) ([]byte, error) {
	object, err := storage.GetObject(context.Background(), config.AppConfig.AppwriteBucketID, key)
	if err != nil {
		return nil, err
	}
	defer object.Body.Close()
	return io.ReadAll(object.Body)
}

func GetManifestFile(_ http.ResponseWriter, videoID string) ([]byte, error) {
	return GetStorageObjectBytes(storagekeys.Manifest(videoID))
}

func DeleteVideo(w http.ResponseWriter, _ *http.Request, db *sql.DB, videoID string) {
	deleteLogger := logger.Log.With("video_id", videoID)
	var rawObjectKey string
	if err := db.QueryRow(`SELECT source_file_key FROM videos WHERE video_id=$1`, videoID).Scan(&rawObjectKey); err != nil {
		deleteLogger.Error("failed to read source object key", "error", err)
		SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	client, err := storage.NewS3Client(context.Background())
	if err != nil {
		deleteLogger.Error("failed to create S3 client", "error", err)
		SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	bucket := config.AppConfig.AppwriteBucketID
	keys := []string{rawObjectKey, storagekeys.Manifest(videoID), storagekeys.Thumbnail(videoID)}
	for _, key := range keys {
		if key == "" {
			continue
		}
		if _, err := client.DeleteObject(context.Background(), &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}); err != nil {
			deleteLogger.Error("failed to delete S3 object", "object_key", key, "error", err)
			SendError(w, http.StatusInternalServerError, "Error deleting video files")
			return
		}
	}
	chunkPrefix := storagekeys.HLSChunkPrefix(videoID)
	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String(chunkPrefix)})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.Background())
		if err != nil {
			deleteLogger.Error("failed to list HLS chunks", "error", err)
			SendError(w, http.StatusInternalServerError, "Error deleting video files")
			return
		}
		for _, object := range page.Contents {
			if object.Key == nil {
				continue
			}
			if _, err := client.DeleteObject(context.Background(), &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: object.Key}); err != nil {
				deleteLogger.Error("failed to delete HLS chunk", "object_key", *object.Key, "error", err)
				SendError(w, http.StatusInternalServerError, "Error deleting video files")
				return
			}
		}
	}
	if _, err := db.Exec(`DELETE FROM videos WHERE video_id=$1`, videoID); err != nil {
		deleteLogger.Error("failed to delete database record", "error", err)
		SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	deleteLogger.Info("video and its storage objects deleted")
}

func GenerateJWT(userID string, username string) (string, error) {
	claims := jwt.MapClaims{
		"user_id":  userID,
		"username": username,
		"exp":      time.Now().Add(time.Hour * 72).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString([]byte(config.AppConfig.JWTSecretKey))
}

func DecodeJWT(tokenString string) (jwt.MapClaims, error) {
	claims := jwt.MapClaims{}

	_, _, err := jwt.NewParser().ParseUnverified(tokenString, claims)
	if err != nil {
		return nil, err
	}

	// Verify expiration
	exp, ok := claims["exp"].(float64)
	if !ok {
		return nil, errors.New("expiration claim missing")
	}
	if time.Since(time.Unix(int64(exp), 0)) > 0 {
		return nil, errors.New("token expired")
	}

	return claims, nil
}

func VerifyToken(tokenString string) (*jwt.Token, error) {
	return jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(config.AppConfig.JWTSecretKey), nil
	})
}

func GetUserFromRequest(r *http.Request) (*types.User, error) {
	authToken, err := r.Cookie("auth_token")
	if err != nil {
		return nil, fmt.Errorf("error getting auth token from request: %w", err)
	}
	db, err := database.GetDBConn()

	if err != nil {
		return nil, fmt.Errorf("error getting database connection: %w", err)
	}

	userRepository := repositories.NewUserRepository(db)
	token, err := VerifyToken(authToken.Value)
	if err != nil {
		return nil, fmt.Errorf("error verifying token: %w", err)
	}
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		userID := claims["user_id"].(string)
		user, err := userRepository.GetUserByID(userID)
		if err != nil {
			return nil, fmt.Errorf("error getting user by ID: %w", err)
		}
		return user, nil
	}
	return nil, nil
}

func extractMetaData(videoPath string) (*types.FFProbeOutput, error) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "stream=codec_name,codec_type",
		"-show_entries", "format=filename,duration,bit_rate,size",
		"-of", "json",
		videoPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("error running ffprobe: %w", err)
	}

	var ffprobeOutput types.FFProbeOutput
	err = json.Unmarshal(output, &ffprobeOutput)
	if err != nil {
		return nil, fmt.Errorf("error parsing ffprobe output: %w", err)
	}

	return &ffprobeOutput, nil
}

func SendError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func UpdateVideoStatus(db *sql.DB, videoID string, status types.VideoStatus) error {
	logger.Log.Info("Updating video status", "video_id", videoID, "status", status)
	switch status {
	case types.ProcessingCompleted:
		return handleProcessingCompletedStatus(db, videoID)
	default:
		return updateGenericVideoStatus(db, videoID, status)
	}
}

func handleProcessingCompletedStatus(db *sql.DB, videoID string) error {

	var query string
	var result sql.Result
	var err error
	query = `
			UPDATE videos
			SET status = $1, upload_end_time = $2
			WHERE video_id = $3;
		`
	result, err = db.Exec(query, types.ProcessingCompleted, time.Now(), videoID)

	if err != nil {
		return fmt.Errorf("failed to update upload status for video %s: %w", videoID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error checking affected rows for video %s: %w", videoID, err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no record found for video_id: %s", videoID)
	}

	return nil
}

func updateGenericVideoStatus(db *sql.DB, videoID string, status types.VideoStatus) error {

	var query string
	var result sql.Result
	var err error
	query = `
			UPDATE videos
			SET status = $1
			WHERE video_id = $2;
		`
	result, err = db.Exec(query, status, videoID)

	if err != nil {
		return fmt.Errorf("failed to update upload status for video %s: %w", videoID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error checking affected rows for video %s: %w", videoID, err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no record found for video_id: %s", videoID)
	}

	return nil
}

func GetRefererPathFromRequest(r *http.Request) (string, error) {
	referer := r.Referer()
	if referer == "" {
		return "", fmt.Errorf("no referer found in request")
	}
	u, err := url.Parse(referer)
	if err != nil {
		return "", fmt.Errorf("error parsing referer URL: %w", err)
	}
	return u.Path, nil
}

func PrettyPrintMap(inputMap any, mapName string) {
	data, err := json.MarshalIndent(inputMap, "", "  ")
	if err != nil {
		log.Printf("error marshaling %s map: %v\n", mapName, err)
		return
	}

	log.Printf("%s:\n%s\n", mapName, data)
}

func Chain(h http.HandlerFunc, middlewares ...func(http.HandlerFunc) http.HandlerFunc) http.HandlerFunc {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}
