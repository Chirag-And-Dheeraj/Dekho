package utils

import (
	"bytes"
	"crypto/sha1"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
	"video-streaming-server/config"
	"video-streaming-server/database"
	"video-streaming-server/repositories"
	"video-streaming-server/shared"
	"video-streaming-server/shared/logger"
	"video-streaming-server/types"

	"github.com/golang-jwt/jwt/v5"
)

var videoProcessing *slog.Logger

func extractThumbnail(videoPath string, fileName string) (string, error) {

	if err := os.Mkdir(fmt.Sprintf("thumbnails/%s", fileName), os.ModePerm); err != nil {
		return "", fmt.Errorf("error creating thumbnail directory: %w", err)
	}

	cmd := exec.Command("ffmpeg", "-y", "-i", videoPath, "-frames:v", "1", config.AppConfig.RootPath+"/thumbnails/"+fileName+"/"+fileName+"_thumbnail.png")

	output, err := cmd.CombinedOutput()

	if err != nil {
		return "", fmt.Errorf("error extracting thumbnail: %w, output: %s", err, string(output))
	}

	return config.AppConfig.RootPath + "/thumbnails/" + fileName + "/" + fileName + "_thumbnail.png", nil
}

func uploadThumbnailToAppwrite(folderName string, db *sql.DB) (string, error) {
	videoProcessing.Debug("Uploading thumbnail of to Appwrite", "video_id", folderName)
	files, err := os.ReadDir(fmt.Sprintf("thumbnails/%s", folderName))

	if err != nil {
		return "", fmt.Errorf("error reading thumbnail directory: %w", err)
	}

	if len(files) == 0 {
		err = os.Remove("thumbnails/" + folderName)
		if err != nil {
			return "", fmt.Errorf("error removing empty thumbnail directory: %w", err)
		}
	}

	fileToUpload, err := os.ReadFile(fmt.Sprintf("thumbnails/%s/%s", folderName, files[0].Name()))

	if err != nil {
		return "", fmt.Errorf("error reading thumbnail file: %w", err)
	}

	uploadRequestURL := "https://cloud.appwrite.io/v1/storage/buckets/" + config.AppConfig.AppwriteBucketID + "/files"

	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)
	fileComps := strings.Split(files[0].Name(), ".")
	fileId := GetFileId(fileComps[0])

	err = writer.WriteField("fileId", fileId)
	if err != nil {
		return "", fmt.Errorf("error writing fileId field: %w", err)
	}

	part, err := writer.CreateFormFile("file", files[0].Name())

	if err != nil {
		return "", fmt.Errorf("error creating form file part: %w", err)
	}

	_, err = part.Write(fileToUpload)

	if err != nil {
		return "", fmt.Errorf("error writing file content to form part: %w", err)
	}

	err = writer.Close()

	if err != nil {
		return "", fmt.Errorf("error closing multipart writer: %w", err)
	}

	request, err := http.NewRequest(http.MethodPost, uploadRequestURL, &requestBody)
	if err != nil {
		return "", fmt.Errorf("error creating request: %w", err)
	}

	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("X-Appwrite-Response-Format", config.AppConfig.AppwriteResponseFormat)
	request.Header.Set("X-Appwrite-Project", config.AppConfig.AppwriteProjectID)
	request.Header.Set("X-Appwrite-Key", config.AppConfig.AppwriteKey)

	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("error sending request to Appwrite: %w", err)
	}
	defer response.Body.Close()

	thumbnailURL := ""

	if response.StatusCode != 201 {
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return "", fmt.Errorf("error reading response body: %w", err)
		}
		videoProcessing.Error("error uploading thumbnail to Appwrite Storage",
			"status_code", response.StatusCode,
			"response_body", string(body),
			"file_name", files[0].Name())
	} else {
		var uploadResponse types.ThumbnailUploadResponse
		err := json.NewDecoder(response.Body).Decode(&uploadResponse)
		if err != nil {
			return "", fmt.Errorf("error decoding response body: %w", err)
		}
		err = os.Remove("thumbnails/" + folderName + "/" + files[0].Name())
		if err != nil {
			return "", fmt.Errorf("error removing thumbnail file after upload: %w", err)
		}

		updateStatement, err := db.Prepare(`
			UPDATE
				videos
			SET
				thumbnail=$1
			WHERE
				video_id=$2;
		`)

		if err != nil {
			return "", fmt.Errorf("error preparing update statement: %w", err)
		}

		thumbnailURL = fmt.Sprintf("https://cloud.appwrite.io/v1/storage/buckets/%s/files/%s/view?project=%s", uploadResponse.BucketID, uploadResponse.ID, config.AppConfig.AppwriteProjectID)

		videoProcessing.Debug("thumbnail URL", "thumbnail_url", thumbnailURL)

		_, err = updateStatement.Exec(thumbnailURL, folderName)
		if err != nil {
			return "", fmt.Errorf("error updating database record: %w", err)
		}

		videoProcessing.Info("thumbnail URL updated in database", "thumbnail_url", thumbnailURL)
	}

	err = os.Remove("thumbnails/" + folderName)
	if err != nil {
		log.Println(err)
		return "", err
	}
	return thumbnailURL, nil
}

func breakFile(videoPath string, fileName string) error {
	videoProcessing.Debug("Breaking file into segments", "video_path", videoPath)

	if err := os.Mkdir(fmt.Sprintf("segments/%s", fileName), os.ModePerm); err != nil {
		return fmt.Errorf("error creating segments directory: %w", err)
	}

	metaData, err := extractMetaData(videoPath)
	videoCodec := ""
	audioCodec := ""
	if err != nil {
		return fmt.Errorf("error extracting metadata: %w", err)
	} else {
		for _, codecs := range metaData.Streams {
			switch codecs.CodecType {
			case "video":
				videoCodec = codecs.CodecName
			case "audio":
				audioCodec = codecs.CodecName
			}
		}

		videoProcessing.Debug("Extracted video and audio codecs", "video_codec", videoCodec, "audio_codec", audioCodec)
	}

	videoCodecAction := "copy"
	audioCodecAction := "copy"

	if videoCodec != "h264" {
		videoProcessing.Info("Converting video codec", "old_video_codec", videoCodec, "new_video_codec", "h264")
		videoCodecAction = "libx264"
	}

	if audioCodec != "aac" {
		videoProcessing.Info("Converting audio codec", "old_audio_codec", audioCodec, "new_audio_codec", "aac")
		audioCodecAction = "aac"
	}

	cmd := exec.Command("ffmpeg", "-y", "-i", videoPath, "-c:v", videoCodecAction, "-preset", "veryfast", "-c:a", audioCodecAction, "-map", "0", "-f", "segment", "-segment_time", "4", "-segment_format", "mpegts", "-segment_list", config.AppConfig.RootPath+"/segments/"+fileName+"/"+fileName+".m3u8", "-segment_list_type", "m3u8", config.AppConfig.RootPath+"/segments/"+fileName+"/"+fileName+"_"+"segment_no_%d.ts")

	output, err := cmd.CombinedOutput()

	if err != nil {
		return fmt.Errorf("error breaking file into segments: %w, output: %s", err, string(output))
	}

	return nil
}

func uploadToAppwrite(folderName string) error {
	// TODO: Add a deferred cleanup function
	files, err := os.ReadDir(fmt.Sprintf("segments/%s", folderName))

	if err != nil {
		return fmt.Errorf("error reading segments directory: %w", err)
	}

	if len(files) == 0 {
		err = os.Remove("segments/" + folderName)
		if err != nil {
			return fmt.Errorf("error removing empty segments directory: %w", err)
		}
	}

	videoProcessing.Debug("Now uploading segments to Appwrite Storage")
	var count int = -1
	for idx, file := range files {
		fileToUpload, err := os.ReadFile(fmt.Sprintf("segments/%s/%s", folderName, file.Name()))

		if err != nil {
			return fmt.Errorf("error reading segment file: %w", err)
		}

		uploadRequestURL := "https://cloud.appwrite.io/v1/storage/buckets/" + config.AppConfig.AppwriteBucketID + "/files"

		var requestBody bytes.Buffer
		writer := multipart.NewWriter(&requestBody)

		fileId := "nil"
		fileComps := strings.Split(file.Name(), ".")
		if fileComps[1] == "m3u8" {
			fileId = fileComps[0]
		} else {
			fileId = GetFileId(fileComps[0])
		}

		err = writer.WriteField("fileId", fileId)

		if err != nil {
			return fmt.Errorf("error writing fileId field: %w", err)
		}

		part, err := writer.CreateFormFile("file", file.Name())

		if err != nil {
			return fmt.Errorf("error creating form file part: %w", err)
		}

		_, err = part.Write(fileToUpload)

		if err != nil {
			return fmt.Errorf("error writing file content to form part: %w", err)
		}

		err = writer.Close()

		if err != nil {
			return fmt.Errorf("error closing multipart writer: %w", err)
		}

		request, err := http.NewRequest(http.MethodPost, uploadRequestURL, &requestBody)
		if err != nil {
			return fmt.Errorf("error creating request: %w", err)
		}

		request.Header.Set("Content-Type", writer.FormDataContentType())
		request.Header.Set("X-Appwrite-Response-Format", config.AppConfig.AppwriteResponseFormat)
		request.Header.Set("X-Appwrite-Project", config.AppConfig.AppwriteProjectID)
		request.Header.Set("X-Appwrite-Key", config.AppConfig.AppwriteKey)

		client := &http.Client{}

		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("error sending request to Appwrite: %w", err)
		}
		defer response.Body.Close()

		if response.StatusCode != 201 {
			body, err := io.ReadAll(response.Body)
			if err != nil {
				return fmt.Errorf("error reading response body: %w", err)
			}
			videoProcessing.Error("error uploading segment to Appwrite Storage", "status_code", response.StatusCode, "response_body", string(body), "file_name", file.Name())
		} else {
			count = idx
			err = os.Remove("segments/" + folderName + "/" + file.Name())
			if err != nil {
				return fmt.Errorf("error removing segment file after upload: %w", err)
			}
		}
	}

	if count == len(files)-1 {
		err = os.Remove("segments/" + folderName)
		if err != nil {
			return fmt.Errorf("error removing segments directory after upload: %w", err)
		}
	}

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
		thumbnailURL, err = uploadThumbnailToAppwrite(fileName, db)
		if err != nil {
			videoProcessing.Error("error uploading thumbnail to appwrite Storage", "error", err)
		}
		videoProcessing.Info("uploaded thumbnail to appwrite Storage", "thumbnail_url", thumbnailURL)
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

	err = uploadToAppwrite(fileName)
	if err != nil {
		videoProcessing.Error("error uploading segments to Appwrite Storage", "error", err)
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

	videoProcessing.Info("uploaded segments to appwrite storage")
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

func GetManifestFile(w http.ResponseWriter, videoId string) ([]byte, error) {

	getManifestFile := "https://cloud.appwrite.io/v1/storage/buckets/" + config.AppConfig.AppwriteBucketID + "/files/" + videoId + "/view"

	request, err := http.NewRequest(http.MethodGet, getManifestFile, nil)

	if err != nil {
		return nil, fmt.Errorf("error creating request to get manifest file: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Appwrite-Response-Format", config.AppConfig.AppwriteResponseFormat)
	request.Header.Set("X-Appwrite-Project", config.AppConfig.AppwriteProjectID)
	request.Header.Set("X-Appwrite-Key", config.AppConfig.AppwriteKey)

	client := &http.Client{}

	response, err := client.Do(request)

	if err != nil {
		return nil, fmt.Errorf("error sending request to get manifest file: %w", err)
	}

	defer response.Body.Close()

	// TODO: util function should not handle HTTP status codes
	if response.StatusCode == 404 {
		SendError(w, http.StatusNotFound, "Manifest file not found")
		return nil, fmt.Errorf("manifest file not found for video ID: %s", videoId)
	}

	bodyBytes, err := io.ReadAll(response.Body)

	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	return bodyBytes, nil
}

func GetFileId(fileName string) string {
	hashChecksum := sha1.New()
	hashChecksum.Write([]byte(fileName))
	fileId := fmt.Sprintf("%x", hashChecksum.Sum(nil))[:36]

	return fileId
}

func DeleteVideo(w http.ResponseWriter, r *http.Request, db *sql.DB, videoId string) {
	// TODO: Use SSEs here
	deleteLogger := logger.Log.With("video_id", videoId)
	fileBytes, err := GetManifestFile(w, videoId)

	if err != nil {
		deleteLogger.Error("Error getting manifest file", "error", err)
		SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	file := string(fileBytes)
	lines := strings.Split(file, "\n")

	deleteUrl := "https://cloud.appwrite.io/v1/storage/buckets/" + config.AppConfig.AppwriteBucketID + "/files/"

	thumbnailFile := videoId + "_thumbnail.png"

	thumbnailFileName := strings.Split(thumbnailFile, ".")[0]

	thumbnailFileId := GetFileId(thumbnailFileName)

	request, err := http.NewRequest(http.MethodDelete, deleteUrl+thumbnailFileId, nil)

	if err != nil {
		deleteLogger.Error("Error creating request to delete thumbnail", "error", err)
		SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Appwrite-Response-Format", config.AppConfig.AppwriteResponseFormat)
	request.Header.Set("X-Appwrite-Project", config.AppConfig.AppwriteProjectID)
	request.Header.Set("X-Appwrite-Key", config.AppConfig.AppwriteKey)

	client := &http.Client{}

	response, err := client.Do(request)
	if err != nil {
		deleteLogger.Error("Error deleting thumbnail file", "error", err)
		SendError(w, http.StatusInternalServerError, "Error Deleting Thumbnail")
		return
	}
	defer response.Body.Close()

	for i := 0; i < len(lines); i++ {
		if strings.HasSuffix(lines[i], ".ts") {
			fileName := strings.Split(lines[i], ".")[0]
			fileId := GetFileId(fileName)

			request, err := http.NewRequest(http.MethodDelete, deleteUrl+fileId, nil)

			if err != nil {
				deleteLogger.Error("Error creating request to delete chunk", "error", err)
				SendError(w, http.StatusInternalServerError, "Internal Server Error")
				return
			}

			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Appwrite-Response-Format", config.AppConfig.AppwriteResponseFormat)
			request.Header.Set("X-Appwrite-Project", config.AppConfig.AppwriteProjectID)
			request.Header.Set("X-Appwrite-Key", config.AppConfig.AppwriteKey)

			client := &http.Client{}

			response, err := client.Do(request)
			if err != nil {
				deleteLogger.Error("Error deleting chunk file", "error", err)
				SendError(w, http.StatusInternalServerError, "Internal Server Error")
				return
			}
			defer response.Body.Close()
		}
	}

	deleteLogger.Info("deleted all .ts files")

	request, err = http.NewRequest(http.MethodDelete, deleteUrl+videoId, nil)

	if err != nil {
		deleteLogger.Error("error creating request to delete manifest file", "error", err)
		SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Appwrite-Response-Format", config.AppConfig.AppwriteResponseFormat)
	request.Header.Set("X-Appwrite-Project", config.AppConfig.AppwriteProjectID)
	request.Header.Set("X-Appwrite-Key", config.AppConfig.AppwriteKey)

	client = &http.Client{}

	response, err = client.Do(request)
	if err != nil {
		deleteLogger.Error("error deleting manifest file", "error", err)
		SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	defer response.Body.Close()

	deleteLogger.Info("deleted .m3u8 file")

	query, err := db.Prepare(`DELETE FROM videos WHERE video_id=$1`)

	if err != nil {
		deleteLogger.Error("error preparing delete query", "error", err)
		SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	_, err = query.Exec(videoId)

	if err != nil {
		deleteLogger.Error("error executing delete query", "error", err)
		SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	deleteLogger.Info("deleted database record")
	deleteLogger.Info("video deleted successfully", "video_id", videoId)
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

func GenerateVerificationLink(email string) (string, error) {
	claims := jwt.MapClaims{
		"email": email,
		"exp":      time.Now().Add(time.Hour * 72).Unix(),
	}

	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	token, err := t.SignedString([]byte(config.AppConfig.JWTSecretKey))
	if err != nil {
		logger.Log.Error(err.Error())

		return "", err
	}

	url := fmt.Sprintf("%s?token=%s", config.AppConfig.Hostname + "/verify", token)

	return url, nil
}
