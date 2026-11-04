package controllers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"video-streaming-server/config"
	"video-streaming-server/shared/logger"
	"video-streaming-server/storage"
	"video-streaming-server/storagekeys"
	"video-streaming-server/types"
	"video-streaming-server/utils"
)

// @desc Get All Videos
// @route GET /video
func GetVideos(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	user, err := utils.GetUserFromRequest(r)

	if err != nil {
		logger.Log.Warn("failed to get user from request", "error", err)
		utils.SendError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	getUserVideosQuery, err := db.Prepare(`
		SELECT
			video_id,
			title,
			description,
			thumbnail,
			status,
			duration_seconds,
			original_video_codec::text,
			original_audio_codec::text,
			original_container::text,
			final_video_codec::text,
			final_audio_codec::text,
			final_container::text,
			video_source::text
		FROM
			videos
		WHERE
			delete_flag=0
		AND
			status <> 0
		AND
			user_id=$1
		ORDER BY
			upload_initiate_time DESC;
	`)

	if err != nil {
		logger.Log.Error("failed to prepare query", "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	rows, err := getUserVideosQuery.Query(user.ID)

	if err != nil {
		logger.Log.Error("failed to execute query", "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	defer rows.Close()

	records := make([]types.VideoResponseType, 0)

	for rows.Next() {
		var id string
		var title string
		var description string
		var thumbnail sql.NullString
		var status types.VideoStatus
		var metadata videoMetadataColumns

		err := rows.Scan(&id, &title, &description, &thumbnail, &status,
			&metadata.duration, &metadata.originalVideo, &metadata.originalAudio, &metadata.originalContainer,
			&metadata.finalVideo, &metadata.finalAudio, &metadata.finalContainer, &metadata.source)

		if err != nil {
			logger.Log.Error("failed to scan row", "error", err)
			utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}

		thumbValue := "../static/logo/android-chrome-192x192.png"
		if thumbnail.Valid && thumbnail.String != "" {
			thumbValue = thumbnail.String
		}

		record := types.VideoResponseType{
			ID:          id,
			Title:       title,
			Description: description,
			Thumbnail:   thumbValue,
			Status:      status,
		}
		applyVideoMetadata(&record, metadata)

		records = append(records, record)
	}

	recordsJSON, err := json.Marshal(records)

	if err != nil {
		logger.Log.Error("failed to marshal records", "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(recordsJSON))
}

// @desc Get a Video
// @route GET /video/[id]
func GetVideo(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	videoId := r.URL.Path[len("/video/"):]

	user, err := utils.GetUserFromRequest(r)

	if err != nil {
		logger.Log.Error("failed to get user from request", "error", err)
		utils.SendError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	detailsQuery, err := db.Prepare(`
		SELECT
			title,description,thumbnail,status,duration_seconds,
			original_video_codec::text,original_audio_codec::text,original_container::text,
			final_video_codec::text,final_audio_codec::text,final_container::text,video_source::text
		FROM
			videos
		WHERE
			video_id=$1
		AND
			user_id=$2;
	`)

	if err != nil {
		logger.Log.Error("failed to prepare query", "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	defer detailsQuery.Close()

	var title, description string
	var thumbnail sql.NullString
	var status types.VideoStatus
	var metadata videoMetadataColumns
	err = detailsQuery.QueryRow(videoId, user.ID).Scan(&title, &description, &thumbnail, &status,
		&metadata.duration, &metadata.originalVideo, &metadata.originalAudio, &metadata.originalContainer,
		&metadata.finalVideo, &metadata.finalAudio, &metadata.finalContainer, &metadata.source)

	if err != nil {
		if err == sql.ErrNoRows {
			logger.Log.Error("video not found", "videoId", videoId, "userId", user.ID)
			utils.SendError(w, http.StatusNotFound, "Video not found")
			return
		}
		logger.Log.Error("failed to query video details", "error", err, "videoId", videoId)
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	videoDetails := &types.VideoResponseType{
		ID:          videoId,
		Title:       title,
		Description: description,
		Status:      status,
	}
	if thumbnail.Valid {
		videoDetails.Thumbnail = thumbnail.String
	}
	applyVideoMetadata(videoDetails, metadata)
	videoDetailsJSON, err := json.Marshal(videoDetails)

	if err != nil {
		logger.Log.Error("failed to marshal response", "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(videoDetailsJSON))
}

type videoMetadataColumns struct {
	duration          sql.NullFloat64
	originalVideo     sql.NullString
	originalAudio     sql.NullString
	originalContainer sql.NullString
	finalVideo        sql.NullString
	finalAudio        sql.NullString
	finalContainer    sql.NullString
	source            string
}

func applyVideoMetadata(response *types.VideoResponseType, metadata videoMetadataColumns) {
	if metadata.duration.Valid {
		value := metadata.duration.Float64
		response.DurationSeconds = &value
	}
	if metadata.originalVideo.Valid {
		response.OriginalVideoCodec = types.VideoCodec(metadata.originalVideo.String)
	}
	if metadata.originalAudio.Valid {
		response.OriginalAudioCodec = types.AudioCodec(metadata.originalAudio.String)
	}
	if metadata.originalContainer.Valid {
		response.OriginalContainer = types.VideoContainer(metadata.originalContainer.String)
	}
	if metadata.finalVideo.Valid {
		response.FinalVideoCodec = types.VideoCodec(metadata.finalVideo.String)
	}
	if metadata.finalAudio.Valid {
		response.FinalAudioCodec = types.AudioCodec(metadata.finalAudio.String)
	}
	if metadata.finalContainer.Valid {
		response.FinalContainer = types.VideoContainer(metadata.finalContainer.String)
	}
	response.VideoSource = types.VideoSource(metadata.source)
}

// @desc Get Manifest File
// @route GET /video/[id]/stream
func ManifestFileHandler(w http.ResponseWriter, r *http.Request) {
	videoId := strings.Split(r.URL.Path[1:], "/")[1]
	if r.URL.Query().Get("inline") == "1" {
		manifest, err := utils.GetManifestFile(w, videoId)
		if err != nil {
			logger.Log.Error("failed to retrieve HLS manifest", "video_id", videoId, "error", err)
			utils.SendError(w, http.StatusInternalServerError, "Unable to retrieve video manifest")
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "private, no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(manifest)
		return
	}
	url, err := storage.PresignGet(r.Context(), config.AppConfig.AppwriteBucketID, storagekeys.Manifest(videoId))
	if err != nil {
		logger.Log.Error("failed to presign HLS manifest", "video_id", videoId, "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Unable to prepare video manifest")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	http.Redirect(w, r, url, http.StatusFound)
}

// @desc Get TS File
// @route GET /video/[id]/stream/[id].ts
func TSFileHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 {
		utils.SendError(w, http.StatusNotFound, "Segment not found")
		return
	}
	videoID, segmentName := parts[1], parts[3]
	url, err := storage.PresignGet(r.Context(), config.AppConfig.AppwriteBucketID, storagekeys.HLSChunk(videoID, segmentName))
	if err != nil {
		logger.Log.Error("failed to presign HLS chunk", "video_id", videoID, "segment", segmentName, "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Unable to prepare video segment")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	http.Redirect(w, r, url, http.StatusFound)
}

func ThumbnailHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	videoID := strings.TrimSuffix(strings.TrimPrefix(path, "/video/"), "/thumbnail")
	var exists bool
	err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM videos WHERE video_id=$1 AND delete_flag=0)`, videoID).Scan(&exists)
	if err != nil {
		logger.Log.Error("failed to look up video for thumbnail", "video_id", videoID, "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Unable to retrieve thumbnail")
		return
	}
	if !exists {
		utils.SendError(w, http.StatusNotFound, "Thumbnail not found")
		return
	}
	thumbnailURL, err := storage.PresignGet(r.Context(), config.AppConfig.AppwriteBucketID, storagekeys.Thumbnail(videoID))
	if err != nil {
		logger.Log.Error("failed to presign thumbnail", "video_id", videoID, "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Unable to retrieve thumbnail")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	http.Redirect(w, r, thumbnailURL, http.StatusFound)
}

// @desc Update Video Details
// @route UPDATE
func UpdateHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	videoId := r.URL.Path[len("/video/"):]
	user, err := utils.GetUserFromRequest(r)
	if err != nil {
		logger.Log.Error("failed to decode request body", "error", err)
		utils.SendError(w, http.StatusBadRequest, "Invalid Request Body")
		return
	}

	if videoId == "" {
		http.Error(w, "Missing 'id' query parameter", http.StatusBadRequest)
		return
	}

	var reqBody types.UpdateRequest
	err = json.NewDecoder(r.Body).Decode(&reqBody)
	if err != nil {
		logger.Log.Warn("failed to get user from request", "error", err)
		utils.SendError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	query, err := db.Prepare(`
		UPDATE
			videos
		SET
			title=$1,
			description=$2
		WHERE
			video_id=$3
		AND
			user_id=$4;
	`)

	if err != nil {
		logger.Log.Error("failed to prepare update query", "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	result, err := query.Exec(reqBody.Title, reqBody.Description, videoId, user.ID)

	if err != nil {
		logger.Log.Error("failed to execute update query", "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logger.Log.Error("failed to get rows affected", "error", err)
		return
	}

	if rowsAffected == 0 {
		logger.Log.Info("video not found for update", "videoId", videoId, "userId", user.ID)
		utils.SendError(w, http.StatusNotFound, "Video not found")
		return
	}

	logger.Log.Info("video details updated", "videoId", videoId, "userId", user.ID)
	w.WriteHeader(http.StatusOK)
}

// @desc Delete the video
// @route DELETE /video/[id]
func DeleteHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	videoId := r.URL.Path[len("/video/"):]

	updateStatement, err := db.Prepare(`
		UPDATE
			videos
		SET
			delete_flag=$1
			WHERE
			video_id=$2;
	`)

	if err != nil {
		logger.Log.Error("failed to prepare update statement", "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	_, err = updateStatement.Exec(1, videoId)

	if err != nil {
		logger.Log.Error("failed to execute update statement", "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	utils.CancelVideoProcessing(videoId)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)

	go utils.DeleteVideo(w, r, db, videoId)
}
