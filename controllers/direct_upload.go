package controllers

import (
	"context"
	"database/sql"
	"encoding/json"
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
	"github.com/google/uuid"
)

type createUploadRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	FileName    string `json:"file_name"`
	FileSize    int64  `json:"file_size"`
	ContentType string `json:"content_type"`
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
	limit, _ := strconv.ParseInt(config.AppConfig.FileSizeLimit, 10, 64)
	if limit > 0 && input.FileSize > limit {
		utils.SendError(w, http.StatusBadRequest, "File size exceeds the configured limit")
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
	_, err = db.Exec(`INSERT INTO videos (video_id,title,description,upload_initiate_time,status,delete_flag,user_id,source_file_key,source_file_size) VALUES ($1,$2,$3,$4,$5,0,$6,$7,$8)`, videoID, input.Title, input.Description, time.Now(), types.UploadPending, user.ID, key, input.FileSize)
	if err != nil {
		logger.Log.Error("failed to create video record", "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	url, err := storage.PresignPut(r.Context(), config.AppConfig.AppwriteBucketID, key, input.ContentType)
	if err != nil {
		logger.Log.Error("failed to presign S3 upload", "error", err)
		_, _ = db.Exec(`DELETE FROM videos WHERE video_id=$1`, videoID)
		utils.SendError(w, http.StatusInternalServerError, "Unable to prepare video upload")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"video_id": videoID, "upload_url": url, "method": http.MethodPut, "content_type": input.ContentType})
}

func CompleteVideoUpload(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	videoID := strings.TrimSuffix(strings.TrimPrefix(path, "/video/"), "/complete")
	user, err := utils.GetUserFromRequest(r)
	if err != nil {
		utils.SendError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	var title, key string
	var expectedSize int64
	err = db.QueryRow(`SELECT title,source_file_key,source_file_size FROM videos WHERE video_id=$1 AND user_id=$2 AND status=$3`, videoID, user.ID, types.UploadPending).Scan(&title, &key, &expectedSize)
	if err == sql.ErrNoRows {
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
	objectSize, err := storage.GetObjectSize(r.Context(), client, config.AppConfig.AppwriteBucketID, key)
	if err != nil {
		logger.Log.Warn("failed to verify uploaded source in S3 storage", "video_id", videoID, "error", err)
		utils.SendError(w, http.StatusConflict, "Uploaded file is not available")
		return
	}
	if objectSize != expectedSize {
		utils.SendError(w, http.StatusConflict, "Uploaded file size does not match the upload request")
		return
	}
	result, err := db.Exec(`UPDATE videos SET status=$1,upload_end_time=$2 WHERE video_id=$3 AND user_id=$4 AND status=$5`, types.UploadedOnServer, time.Now(), videoID, user.ID, types.UploadPending)
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
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("Video upload confirmed and processing started"))
	go downloadAndProcessVideo(context.Background(), client, key, videoID, title, db, types.UserID(user.ID))
}

func downloadAndProcessVideo(ctx context.Context, client *s3.Client, key, videoID, title string, db *sql.DB, userID types.UserID) {
	object, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(config.AppConfig.AppwriteBucketID), Key: aws.String(key)})
	if err != nil {
		logger.Log.Error("failed to download source from S3 storage", "video_id", videoID, "error", err)
		_ = utils.UpdateVideoStatus(db, videoID, types.ProcessingFailed)
		return
	}
	defer object.Body.Close()
	if err := os.MkdirAll("./video", 0755); err != nil {
		logger.Log.Error("failed to prepare video directory", "error", err)
		_ = utils.UpdateVideoStatus(db, videoID, types.ProcessingFailed)
		return
	}
	tmpFile, err := os.Create(filepath.Join("./video", filepath.Base(key)))
	if err != nil {
		logger.Log.Error("failed to create local source file", "error", err)
		_ = utils.UpdateVideoStatus(db, videoID, types.ProcessingFailed)
		return
	}
	if _, err = io.Copy(tmpFile, object.Body); err != nil {
		logger.Log.Error("failed to download source", "error", err)
		tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		_ = utils.UpdateVideoStatus(db, videoID, types.ProcessingFailed)
		return
	}
	if _, err = tmpFile.Seek(0, io.SeekStart); err != nil {
		logger.Log.Error("failed to rewind source", "error", err)
		tmpFile.Close()
		_ = utils.UpdateVideoStatus(db, videoID, types.ProcessingFailed)
		return
	}
	utils.PostUploadProcessFile(filepath.Base(key), videoID, title, tmpFile, db, userID)
}
