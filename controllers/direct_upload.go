package controllers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"video-streaming-server/config"
	"video-streaming-server/shared/logger"
	"video-streaming-server/storage"
	"video-streaming-server/storagekeys"
	"video-streaming-server/types"
	"video-streaming-server/utils"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/google/uuid"
)

const (
	abandonedUploadTTL      = 24 * time.Hour
	abandonedUploadInterval = 15 * time.Minute
)

type createUploadRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	FileName    string `json:"file_name"`
	FileSize    int64  `json:"file_size"`
	ContentType string `json:"content_type"`
}

type completeUploadRequest struct {
	Parts []completedUploadPart `json:"parts"`
}

type completedUploadPart struct {
	PartNumber int32  `json:"part_number"`
	ETag       string `json:"etag"`
}

type uploadPartURL struct {
	PartNumber int32  `json:"part_number"`
	UploadURL  string `json:"upload_url"`
}

func CreateVideoUpload(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	user, err := utils.GetUserFromRequest(r)
	if err != nil {
		utils.SendError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	var input createUploadRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&input); err != nil {
		utils.SendError(w, http.StatusBadRequest, "Invalid upload request")
		return
	}
	if strings.TrimSpace(input.Title) == "" || input.FileSize <= 0 || filepath.Base(input.FileName) != input.FileName || input.FileName == "" {
		utils.SendError(w, http.StatusBadRequest, "Title, valid file name, and positive file size are required")
		return
	}
	allowed := map[string]bool{"video/mp4": true, "video/x-matroska": true, "video/quicktime": true}
	if !allowed[input.ContentType] {
		utils.SendError(w, http.StatusBadRequest, "Unsupported video type")
		return
	}
	limit, err := strconv.ParseInt(config.AppConfig.FileSizeLimit, 10, 64)
	if err != nil {
		logger.Log.Error("invalid FILE_SIZE_LIMIT configuration", "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Upload service is not configured correctly")
		return
	}
	if limit <= 0 {
		logger.Log.Error("invalid FILE_SIZE_LIMIT configuration", "reason", "must be a positive integer")
		utils.SendError(w, http.StatusInternalServerError, "Upload service is not configured correctly")
		return
	}
	if input.FileSize > limit {
		utils.SendError(w, http.StatusRequestEntityTooLarge, "File too large.")
		return
	}
	if (input.FileSize-1)/storage.MultipartPartSize+1 > 10000 {
		utils.SendError(w, http.StatusBadRequest, "File size exceeds the multipart upload limit")
		return
	}
	ext := strings.ToLower(filepath.Ext(input.FileName))
	validExt := map[string]bool{".mp4": true, ".mkv": true, ".mov": true}
	if !validExt[ext] {
		utils.SendError(w, http.StatusBadRequest, "Unsupported video file extension")
		return
	}
	videoID := uuid.NewString()
	key := storagekeys.RawFile(videoID, ext)
	uploadID, err := storage.CreateMultipartUpload(r.Context(), config.AppConfig.AppwriteBucketID, key, input.ContentType)
	if err != nil {
		logger.Log.Error("failed to create multipart upload", "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Unable to prepare video upload")
		return
	}
	_, err = db.Exec(`INSERT INTO videos (video_id,title,description,upload_initiate_time,status,delete_flag,user_id,source_file_key,source_file_size,source_upload_id) VALUES ($1,$2,$3,$4,$5,0,$6,$7,$8,$9)`, videoID, input.Title, input.Description, time.Now(), types.UploadPending, user.ID, key, input.FileSize, uploadID)
	if err != nil {
		logger.Log.Error("failed to create video record", "error", err)
		if client, clientErr := storage.NewS3Client(r.Context()); clientErr == nil {
			if abortErr := storage.AbortMultipartUpload(r.Context(), client, config.AppConfig.AppwriteBucketID, key, uploadID); abortErr != nil {
				logger.Log.Error("failed to abort untracked multipart upload", "error", abortErr)
			}
		} else {
			logger.Log.Error("failed to create S3 client to abort untracked upload", "error", clientErr)
		}
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	partCount := int((input.FileSize-1)/storage.MultipartPartSize + 1)
	partURLs := make([]uploadPartURL, partCount)
	for i := range partURLs {
		partNumber := int32(i + 1)
		uploadURL, presignErr := storage.PresignUploadPart(r.Context(), config.AppConfig.AppwriteBucketID, key, uploadID, partNumber)
		if presignErr != nil {
			logger.Log.Error("failed to presign multipart upload part", "video_id", videoID, "part_number", partNumber, "error", presignErr)
			if client, clientErr := storage.NewS3Client(r.Context()); clientErr == nil {
				if abortErr := storage.AbortMultipartUpload(r.Context(), client, config.AppConfig.AppwriteBucketID, key, uploadID); abortErr != nil {
					logger.Log.Error("failed to abort multipart upload after presigning error", "video_id", videoID, "error", abortErr)
				}
			}
			if _, deleteErr := db.Exec(`DELETE FROM videos WHERE video_id=$1 AND user_id=$2 AND status=$3`, videoID, user.ID, types.UploadPending); deleteErr != nil {
				logger.Log.Error("failed to remove upload after presigning error", "video_id", videoID, "error", deleteErr)
			}
			utils.SendError(w, http.StatusInternalServerError, "Unable to prepare video upload")
			return
		}
		partURLs[i] = uploadPartURL{PartNumber: partNumber, UploadURL: uploadURL}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"video_id": videoID, "part_size": storage.MultipartPartSize, "parts": partURLs})
}

func AbortVideoUpload(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	user, err := utils.GetUserFromRequest(r)
	if err != nil {
		utils.SendError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) != 3 {
		utils.SendError(w, http.StatusBadRequest, "Invalid upload request")
		return
	}
	videoID := pathParts[1]
	releaseVideoLock, err := utils.AcquireVideoLock(r.Context(), db, videoID)
	if err != nil {
		logger.Log.Error("failed to lock video upload for abort", "video_id", videoID, "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Unable to cancel upload")
		return
	}
	defer releaseVideoLock()
	var key, uploadID string
	err = db.QueryRow(`SELECT source_file_key,source_upload_id FROM videos WHERE video_id=$1 AND user_id=$2 AND status=$3 AND delete_flag=0`, videoID, user.ID, types.UploadPending).Scan(&key, &uploadID)
	if err == sql.ErrNoRows {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		logger.Log.Error("failed to find pending upload to abort", "video_id", videoID, "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	client, err := storage.NewS3Client(r.Context())
	if err != nil {
		logger.Log.Error("failed to create S3 client to abort upload", "video_id", videoID, "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	if err := storage.AbortMultipartUpload(r.Context(), client, config.AppConfig.AppwriteBucketID, key, uploadID); err != nil {
		logger.Log.Error("failed to abort multipart upload", "video_id", videoID, "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Unable to cancel upload")
		return
	}
	if _, err := db.Exec(`DELETE FROM videos WHERE video_id=$1 AND user_id=$2 AND status=$3`, videoID, user.ID, types.UploadPending); err != nil {
		logger.Log.Error("failed to delete aborted upload record", "video_id", videoID, "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Unable to cancel upload")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// StartAbandonedUploadCleanup periodically removes multipart uploads that were
// created but never completed. The row's initiation time is the expiry clock.
func StartAbandonedUploadCleanup(db *sql.DB) {
	go func() {
		cleanupAbandonedUploads(db)
		ticker := time.NewTicker(abandonedUploadInterval)
		defer ticker.Stop()
		for range ticker.C {
			cleanupAbandonedUploads(db)
		}
	}()
}

func cleanupAbandonedUploads(db *sql.DB) {
	cutoff := time.Now().Add(-abandonedUploadTTL)
	rows, err := db.Query(`SELECT video_id FROM videos WHERE status=$1 AND upload_initiate_time <= $2 ORDER BY upload_initiate_time ASC LIMIT 100`, types.UploadPending, cutoff)
	if err != nil {
		logger.Log.Error("failed to find abandoned uploads", "error", err)
		return
	}
	var videoIDs []string
	for rows.Next() {
		var videoID string
		if err := rows.Scan(&videoID); err != nil {
			logger.Log.Error("failed to read abandoned upload", "error", err)
			continue
		}
		videoIDs = append(videoIDs, videoID)
	}
	if err := rows.Err(); err != nil {
		logger.Log.Error("failed while reading abandoned uploads", "error", err)
	}
	_ = rows.Close()

	for _, videoID := range videoIDs {
		if err := cleanupAbandonedUpload(db, videoID, cutoff); err != nil {
			logger.Log.Error("failed to clean abandoned upload", "video_id", videoID, "error", err)
		}
	}
}

func cleanupAbandonedUpload(db *sql.DB, videoID string, cutoff time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	releaseVideoLock, err := utils.AcquireVideoLock(ctx, db, videoID)
	if err != nil {
		return fmt.Errorf("lock video upload: %w", err)
	}
	defer releaseVideoLock()

	var key, uploadID sql.NullString
	err = db.QueryRowContext(ctx, `SELECT source_file_key,source_upload_id FROM videos WHERE video_id=$1 AND status=$2 AND upload_initiate_time <= $3`, videoID, types.UploadPending, cutoff).Scan(&key, &uploadID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read pending upload: %w", err)
	}

	client, err := storage.NewS3Client(ctx)
	if err != nil {
		return fmt.Errorf("create S3 client: %w", err)
	}
	if key.Valid && key.String != "" && uploadID.Valid && uploadID.String != "" {
		if err := storage.AbortMultipartUpload(ctx, client, config.AppConfig.AppwriteBucketID, key.String, uploadID.String); err != nil {
			var apiErr smithy.APIError
			if !errors.As(err, &apiErr) || apiErr.ErrorCode() != "NoSuchUpload" {
				return fmt.Errorf("abort multipart upload: %w", err)
			}
		}
	}
	if key.Valid && key.String != "" {
		if _, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(config.AppConfig.AppwriteBucketID), Key: aws.String(key.String)}); err != nil {
			return fmt.Errorf("delete source object: %w", err)
		}
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM videos WHERE video_id=$1 AND status=$2 AND upload_initiate_time <= $3`, videoID, types.UploadPending, cutoff); err != nil {
		return fmt.Errorf("delete expired upload record: %w", err)
	}
	logger.Log.Info("abandoned upload and its storage objects deleted", "video_id", videoID)
	return nil
}

func CompleteVideoUpload(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	videoID := strings.TrimSuffix(strings.TrimPrefix(path, "/video/"), "/complete")
	user, err := utils.GetUserFromRequest(r)
	if err != nil {
		utils.SendError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	var currentStatus types.VideoStatus
	err = db.QueryRow(`SELECT status FROM videos WHERE video_id=$1 AND user_id=$2 AND delete_flag=0`, videoID, user.ID).Scan(&currentStatus)
	if err == sql.ErrNoRows {
		utils.SendError(w, http.StatusNotFound, "Pending upload not found")
		return
	}
	if err != nil {
		logger.Log.Error("failed to check upload status", "video_id", videoID, "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	if currentStatus != types.UploadPending {
		writeUploadAccepted(w)
		return
	}
	var request completeUploadRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024)).Decode(&request); err != nil {
		utils.SendError(w, http.StatusBadRequest, "Invalid upload completion request")
		return
	}
	releaseVideoLock, err := utils.AcquireVideoLock(r.Context(), db, videoID)
	if err != nil {
		logger.Log.Error("failed to lock video upload for completion", "video_id", videoID, "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Unable to complete upload")
		return
	}
	defer releaseVideoLock()
	var title, key, uploadID string
	var expectedSize int64
	err = db.QueryRow(`SELECT title,source_file_key,source_file_size,source_upload_id FROM videos WHERE video_id=$1 AND user_id=$2 AND status=$3 AND delete_flag=0`, videoID, user.ID, types.UploadPending).Scan(&title, &key, &expectedSize, &uploadID)
	if err == sql.ErrNoRows {
		var status types.VideoStatus
		advancedErr := db.QueryRow(`SELECT status FROM videos WHERE video_id=$1 AND user_id=$2 AND delete_flag=0`, videoID, user.ID).Scan(&status)
		if advancedErr == nil {
			if status != types.UploadPending {
				writeUploadAccepted(w)
			} else {
				utils.SendError(w, http.StatusNotFound, "Pending upload not found")
			}
			return
		}
		if advancedErr != sql.ErrNoRows {
			logger.Log.Error("failed to check completion retry status", "video_id", videoID, "error", advancedErr)
			utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		utils.SendError(w, http.StatusNotFound, "Pending upload not found")
		return
	}
	if err != nil {
		logger.Log.Error("failed to find pending upload", "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	client, err := storage.NewS3Client(r.Context())
	if err != nil {
		logger.Log.Error("failed to create S3 client", "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	expectedParts := (expectedSize-1)/storage.MultipartPartSize + 1
	if len(request.Parts) != int(expectedParts) {
		utils.SendError(w, http.StatusBadRequest, "Upload is missing one or more parts")
		return
	}
	completedParts := make([]s3types.CompletedPart, len(request.Parts))
	for i, part := range request.Parts {
		if part.PartNumber != int32(i+1) || strings.TrimSpace(part.ETag) == "" {
			utils.SendError(w, http.StatusBadRequest, "Upload parts must be complete and ordered")
			return
		}
		completedParts[i] = s3types.CompletedPart{PartNumber: aws.Int32(part.PartNumber), ETag: aws.String(part.ETag)}
	}
	storedParts, listErr := client.ListParts(r.Context(), &s3.ListPartsInput{
		Bucket: aws.String(config.AppConfig.AppwriteBucketID), Key: aws.String(key), UploadId: aws.String(uploadID), MaxParts: aws.Int32(1000),
	})
	if listErr != nil {
		logger.Log.Error("failed to list multipart parts before completion", "video_id", videoID, "upload_id", uploadID, "error", listErr)
		utils.SendError(w, http.StatusBadGateway, "File upload failed. Check the file and its size, then try again later.")
		return
	}
	storedETags := make(map[int32]string, len(storedParts.Parts))
	for _, part := range storedParts.Parts {
		if part.PartNumber != nil && part.ETag != nil {
			storedETags[*part.PartNumber] = *part.ETag
		}
	}
	partsMatch := len(storedETags) == len(request.Parts)
	for _, part := range request.Parts {
		if storedETags[part.PartNumber] != part.ETag {
			partsMatch = false
			break
		}
	}
	if !partsMatch {
		logger.Log.Warn("uploaded parts do not match completion request", "video_id", videoID, "upload_id", uploadID, "stored_part_count", len(storedETags), "client_part_count", len(request.Parts))
		utils.SendError(w, http.StatusConflict, "Uploaded parts do not match; please retry the upload")
		return
	}
	_, err = client.CompleteMultipartUpload(r.Context(), &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(config.AppConfig.AppwriteBucketID), Key: aws.String(key), UploadId: aws.String(uploadID),
		MultipartUpload: &s3types.CompletedMultipartUpload{Parts: completedParts},
	})
	if err != nil {
		logger.Log.Error("failed to complete multipart upload", "video_id", videoID, "upload_id", uploadID, "key", key, "client_part_count", len(completedParts), "error", err)
		if abortErr := storage.AbortMultipartUpload(r.Context(), client, config.AppConfig.AppwriteBucketID, key, uploadID); abortErr == nil {
			if _, deleteErr := db.Exec(`DELETE FROM videos WHERE video_id=$1 AND user_id=$2 AND status=$3`, videoID, user.ID, types.UploadPending); deleteErr != nil {
				logger.Log.Error("failed to remove rejected upload record", "video_id", videoID, "error", deleteErr)
			}
		} else {
			logger.Log.Warn("failed to abort rejected multipart upload", "video_id", videoID, "error", abortErr)
		}
		utils.SendError(w, http.StatusBadGateway, "File upload failed. Check the file and its size, then try again later.")
		return
	}
	objectSize, err := storage.GetObjectSize(r.Context(), client, config.AppConfig.AppwriteBucketID, key)
	if err != nil {
		logger.Log.Warn("failed to verify uploaded source in S3 storage", "video_id", videoID, "error", err)
		if _, deleteErr := client.DeleteObject(r.Context(), &s3.DeleteObjectInput{Bucket: aws.String(config.AppConfig.AppwriteBucketID), Key: aws.String(key)}); deleteErr == nil {
			if _, rowErr := db.Exec(`DELETE FROM videos WHERE video_id=$1 AND user_id=$2 AND status=$3`, videoID, user.ID, types.UploadPending); rowErr != nil {
				logger.Log.Error("failed to remove unverified upload record", "video_id", videoID, "error", rowErr)
			}
		} else {
			logger.Log.Error("failed to remove unverified upload object", "video_id", videoID, "error", deleteErr)
		}
		utils.SendError(w, http.StatusConflict, "Uploaded file is not available")
		return
	}
	if objectSize != expectedSize {
		if _, deleteErr := client.DeleteObject(r.Context(), &s3.DeleteObjectInput{Bucket: aws.String(config.AppConfig.AppwriteBucketID), Key: aws.String(key)}); deleteErr != nil {
			logger.Log.Error("failed to remove upload with unexpected size", "video_id", videoID, "error", deleteErr)
			utils.SendError(w, http.StatusInternalServerError, "Uploaded file size does not match; storage cleanup failed")
			return
		}
		if _, deleteErr := db.Exec(`DELETE FROM videos WHERE video_id=$1 AND user_id=$2 AND status=$3`, videoID, user.ID, types.UploadPending); deleteErr != nil {
			logger.Log.Error("failed to remove mismatched upload record", "video_id", videoID, "error", deleteErr)
		}
		utils.SendError(w, http.StatusConflict, "Uploaded file size does not match the upload request")
		return
	}
	result, err := db.Exec(`UPDATE videos SET status=$1,upload_end_time=$2 WHERE video_id=$3 AND user_id=$4 AND status=$5 AND delete_flag=0`, types.UploadedOnServer, time.Now(), videoID, user.ID, types.UploadPending)
	if err != nil {
		logger.Log.Error("failed to update video status", "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		utils.SendError(w, http.StatusConflict, "Upload has already been confirmed")
		return
	}
	writeUploadAccepted(w)
	go downloadAndProcessVideo(context.Background(), client, key, videoID, title, db, types.UserID(user.ID))
}

func writeUploadAccepted(w http.ResponseWriter) {
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("Video upload confirmed and processing started"))
}

func downloadAndProcessVideo(ctx context.Context, client *s3.Client, key, videoID, title string, db *sql.DB, userID types.UserID) {
	ctx, finishProcessing, err := utils.BeginVideoProcessing(ctx, db, videoID)
	if err != nil {
		logger.Log.Info("video processing skipped", "video_id", videoID, "error", err)
		return
	}
	defer finishProcessing()

	object, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(config.AppConfig.AppwriteBucketID), Key: aws.String(key)})
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		logger.Log.Error("failed to download source from S3 storage", "video_id", videoID, "error", err)
		_ = utils.UpdateVideoStatus(db, videoID, types.ProcessingFailed)
		return
	}
	defer object.Body.Close()
	if err := os.MkdirAll("./video", 0755); err != nil {
		if ctx.Err() != nil {
			return
		}
		logger.Log.Error("failed to prepare video directory", "error", err)
		_ = utils.UpdateVideoStatus(db, videoID, types.ProcessingFailed)
		return
	}
	tmpFile, err := os.Create(filepath.Join("./video", filepath.Base(key)))
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		logger.Log.Error("failed to create local source file", "error", err)
		_ = utils.UpdateVideoStatus(db, videoID, types.ProcessingFailed)
		return
	}
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
	}()
	if _, err = io.Copy(tmpFile, object.Body); err != nil {
		logger.Log.Error("failed to download source", "error", err)
		tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		if ctx.Err() != nil {
			return
		}
		_ = utils.UpdateVideoStatus(db, videoID, types.ProcessingFailed)
		return
	}
	if _, err = tmpFile.Seek(0, io.SeekStart); err != nil {
		logger.Log.Error("failed to rewind source", "error", err)
		tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		if ctx.Err() != nil {
			return
		}
		_ = utils.UpdateVideoStatus(db, videoID, types.ProcessingFailed)
		return
	}
	utils.PostUploadProcessFile(ctx, filepath.Base(key), videoID, title, tmpFile, db, userID)
}
