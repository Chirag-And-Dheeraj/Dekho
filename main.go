package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	"video-streaming-server/config"
	"video-streaming-server/controllers"
	"video-streaming-server/database"
	mw "video-streaming-server/middleware"
	"video-streaming-server/queueing"
	"video-streaming-server/repositories"
	"video-streaming-server/services"
	"video-streaming-server/shared"
	"video-streaming-server/shared/logger"
	"video-streaming-server/shared/redisconn"
	"video-streaming-server/sse"
	"video-streaming-server/statusbus"
	"video-streaming-server/types"
	"video-streaming-server/utils"
)

/*
/video/ - Get All Videos
/video/[id] - Get A Video
/video/[id]/stream - Get The Manifest File For The Video
/video/[id]/stream/[filename] - Get The Segment of Video
*/

func videoHandler(w http.ResponseWriter, r *http.Request, queueClient *asynq.Client) {
	path := r.URL.Path
	method := r.Method
	if method == http.MethodGet {
		if matched, _ := regexp.MatchString(`^/video/[a-zA-Z0-9-]+/stream/?$`, path); matched {
			controllers.ManifestFileHandler(w, r)
			return
		}
		if matched, _ := regexp.MatchString(`^/video/[a-zA-Z0-9-]+/stream/[a-zA-Z0-9_-]+\.ts/?$`, path); matched {
			controllers.TSFileHandler(w, r)
			return
		}
	}
	db, err := database.GetDBConn()

	if err != nil {
		logger.Log.Error("failed to get database connection", "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	if method == http.MethodPost {
		if matched, _ := regexp.MatchString(`^/video/[a-zA-Z0-9-]+/abort/?$`, path); matched {
			controllers.AbortVideoUpload(w, r, db)
		} else if matched, _ := regexp.MatchString(`^/video/[a-zA-Z0-9-]+/complete/?$`, path); matched {
			controllers.CompleteVideoUpload(w, r, db, queueClient)
		} else if path == "/video/" {
			controllers.CreateVideoUpload(w, r, db)
		} else {
			utils.SendError(w, http.StatusNotFound, "Not Found")
		}
	} else if method == http.MethodGet {
		if path == "/video/" {
			controllers.GetVideos(w, r, db)
		} else if matched, err := regexp.MatchString(`^/video/[a-zA-Z0-9-]+/thumbnail/?$`, path); err == nil && matched {
			controllers.ThumbnailHandler(w, r, db)
		} else if matched, err := regexp.MatchString("^/video/[a-zA-B0-9-]+/?$", path); err == nil && matched {
			controllers.GetVideo(w, r, db)
		} else {
			response := fmt.Sprintf("Error: handler for %s not found", html.EscapeString(r.URL.Path))
			http.Error(w, response, http.StatusNotFound)
		}
	} else if method == http.MethodDelete {
		if matched, err := regexp.MatchString("^/video/[a-zA-B0-9-]+/?$", path); err == nil && matched {
			controllers.DeleteHandler(w, r, db)
		}
	} else if method == http.MethodPatch {
		if matched, err := regexp.MatchString("^/video/[a-zA-B0-9-]+/?$", path); err == nil && matched {
			controllers.UpdateHandler(w, r, db)
		}
	}
}

func homePageHandler(w http.ResponseWriter, r *http.Request) {

	if r.URL.Path != "/" {
		logger.Log.Info("request path", "path", r.URL.Path)
		utils.SendError(w, http.StatusNotFound, "Not Found")
		return
	}

	if r.Method != http.MethodGet {
		utils.SendError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}

	tmpl := template.Must(template.ParseFiles("./client/index.html"))

	isLoggedIn := false
	username := ""
	cookie, err := r.Cookie("auth_token")
	if err == nil && cookie != nil {
		claims, err := utils.DecodeJWT(cookie.Value)
		if err == nil {
			isLoggedIn = true
			username = claims["username"].(string)
		}
	}

	tmpl.Execute(w, types.HomePageData{
		IsLoggedIn: isLoggedIn,
		Username:   username,
	})
}

func registerPageHandler(w http.ResponseWriter, r *http.Request) {
	db, err := database.GetDBConn()

	if err != nil {
		logger.Log.Error("failed to get database connection", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if r.Method == http.MethodPost {
		userRepository := repositories.NewUserRepository(db)
		userService := services.NewUserService(userRepository)
		controllers.RegisterUser(w, r, userService)
	} else if r.Method == http.MethodGet {
		p := "./client/register.html"
		http.ServeFile(w, r, p)
	}
}

func loginPageHandler(w http.ResponseWriter, r *http.Request) {
	db, err := database.GetDBConn()

	if err != nil {
		logger.Log.Error("failed to get database connection", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if r.Method == http.MethodPost {
		userRepository := repositories.NewUserRepository(db)
		userService := services.NewUserService(userRepository)
		controllers.LoginUser(w, r, userService)
	} else if r.Method == http.MethodGet {
		p := "./client/login.html"
		http.ServeFile(w, r, p)
	}
}

func uploadPageHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method == http.MethodGet {
		p := "./client/upload.html"
		http.ServeFile(w, r, p)
	}
}

func listPageHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method == http.MethodGet {
		p := "./client/list.html"
		http.ServeFile(w, r, p)
	}
}

func watchPageHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method == http.MethodGet {
		p := "./client/watch.html"
		http.ServeFile(w, r, p)
	}
}

func configHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodGet {
		logger.Log.Warn("unsupported method for config endpoint", "method", r.Method)
		utils.SendError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}

	fileSizeLimit := config.AppConfig.FileSizeLimit
	if fileSizeLimit == "" {
		logger.Log.Error("file size limit is not set in config")
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	supportedFileTypes := []config.FileType{
		{FileType: "video/mp4", FileExtension: ".mp4"},
		{FileType: "video/x-matroska", FileExtension: ".mkv"},
		{FileType: "video/quicktime", FileExtension: ".mov"},
	}

	response := config.ConfigResponse{
		FileSizeLimit:      fileSizeLimit,
		SupportedFileTypes: supportedFileTypes,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(response)
	if err != nil {
		logger.Log.Error("failed to encode config response", "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
	}
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method == http.MethodPost {
		http.SetCookie(w, &http.Cookie{
			Name:     "auth_token",
			Value:    "",
			Expires:  time.Now(),
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}
}

func serverSentEventsHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: talk to Jaden about security review
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method != http.MethodGet {
		utils.SendError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}

	user, err := utils.GetUserFromRequest(r)
	if err != nil {
		logger.Log.Error("failed to extract user from request", "error", err)
		utils.SendError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	path, err := utils.GetRefererPathFromRequest(r)
	if err != nil {
		logger.Log.Error("failed to extract referer path from request", "error", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		utils.SendError(w, http.StatusBadRequest, "Bad Request")
		return
	}
	userID := types.UserID(user.ID)
	sessionID := sse.InitializeSSEConnection(userID, path)
	logger.Log.Debug("session initialized", "sessionID", sessionID)

	logger.Log.Info("user connected to event stream", "username", user.Username)

	// defer sachMeDeferWaleFunctionsReturnKePehleExecuteHoteHainTestingFunction()
	defer sse.RemoveSSEConnection(userID, sessionID)

	logger.Log.Info("user connected to the event stream", "user id", userID, "session id", sessionID)

	disconnected := r.Context().Done()
	rc := http.NewResponseController(w)

	// the intent behind this flush is for an initial connection heartbeat
	rc.Flush()

	channel, ok := shared.GetSSEChannel(userID, sessionID)
	if !ok {
		logger.Log.Error("SSE session channel disappeared during setup", "user_id", userID, "session_id", sessionID)
		return
	}

	for {
		select {
		case <-disconnected:
			logger.Log.Info("user disconnected from event stream", "user id", userID, "session id", sessionID)
			return
		case rawMessage, open := <-channel.EventChannel:
			if !open {
				return
			}

			message := rawMessage
			event := message.Event
			rawData := message.Data
			eventData, err := json.Marshal(rawData)
			if err != nil {
				logger.Log.Error("failed to marshal event data", "error", err)
				continue
			}
			logger.Log.Debug("sending event data to user on session", "event", event, "event data", eventData, "user id", userID, "session id", sessionID)
			frame := ""
			if message.ID != "" {
				frame = fmt.Sprintf("id: %s\n", message.ID)
			}
			frame += fmt.Sprintf("event: %s\ndata: %s\n\n", event, eventData)
			if n, err := fmt.Fprint(w, frame); err != nil {
				logger.Log.Error("failed to send event data to user on session", "event", event, "event data", eventData, "user id", userID, "session id", sessionID, "error", err)
				return
			} else {
				logger.Log.Info("sent event data to user on session", "event", event, "event data", eventData, "user id", userID, "session id", sessionID, "bytes written", n)
			}

			rc.Flush()
		}
	}
}

func setUpRoutes(queueClient *asynq.Client) {
	logger.Log.Debug("setting up routes")
	http.HandleFunc("/healthz", shared.HealthHandler)
	http.HandleFunc("/", utils.Chain(homePageHandler, mw.Logging))
	http.HandleFunc("/register", utils.Chain(registerPageHandler, mw.Logging))
	http.HandleFunc("/login", utils.Chain(loginPageHandler, mw.Logging))
	http.HandleFunc("/logout", utils.Chain(logoutHandler, mw.Logging))
	http.HandleFunc("/upload", utils.Chain(uploadPageHandler, mw.Logging, mw.AuthRequired))
	http.HandleFunc("/list", utils.Chain(listPageHandler, mw.Logging, mw.AuthRequired))
	http.HandleFunc("/watch", utils.Chain(watchPageHandler, mw.Logging, mw.AuthRequired))
	http.HandleFunc("/config", configHandler)
	http.HandleFunc("/video/", utils.Chain(func(w http.ResponseWriter, r *http.Request) {
		videoHandler(w, r, queueClient)
	}, mw.Logging, mw.AuthRequired))
	http.HandleFunc("/server-events/", utils.Chain(serverSentEventsHandler, mw.Logging, mw.AuthRequired))

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	logger.Log.Info("routes set up successfully")
}

func main() {

	if err := config.LoadConfig(".env"); err != nil {
		slog.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	logger.Init(config.AppConfig.Debug)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cleanupDB, dbErr := database.GetDBConn()
	if dbErr != nil {
		slog.Error("Failed to connect to database", "error", dbErr)
		os.Exit(1)
	}
	defer cleanupDB.Close()
	redisOptions, err := redisconn.GoRedisOptions()
	if err != nil {
		slog.Error("Failed to configure Redis", "error", err)
		os.Exit(1)
	}
	redisClient := redis.NewClient(redisOptions)
	redisCtx, redisCancel := context.WithTimeout(ctx, 5*time.Second)
	if err := redisClient.Ping(redisCtx).Err(); err != nil {
		redisCancel()
		slog.Error("Failed to connect to Redis", "error", err)
		os.Exit(1)
	}
	redisCancel()
	defer redisClient.Close()
	asynqRedisOptions, err := redisconn.AsynqOptions()
	if err != nil {
		slog.Error("Failed to configure Asynq Redis connection", "error", err)
		os.Exit(1)
	}
	asynqClient := asynq.NewClient(asynqRedisOptions)
	defer asynqClient.Close()
	bus, err := statusbus.New(cleanupDB, redisClient)
	if err != nil {
		slog.Error("Failed to initialize status event bus", "error", err)
		os.Exit(1)
	}
	go func() {
		if err := bus.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Log.Error("status event bus stopped", "error", err)
		}
	}()
	go reconcileVideoProcessingQueue(ctx, cleanupDB, asynqClient)
	controllers.StartAbandonedUploadCleanup(cleanupDB)
	setUpRoutes(asynqClient)
	logger.Log.Info(
		"Dekho server is listening on",
		"address", config.AppConfig.Addr,
		"port", config.AppConfig.Port,
	)
	server := &http.Server{Addr: fmt.Sprintf("%s:%s", config.AppConfig.Addr, config.AppConfig.Port), Handler: http.DefaultServeMux}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Log.Error("failed to gracefully stop API server", "error", err)
		}
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			slog.Error("server failed to start", "error", err)
			os.Exit(1)
		}
	}
}

func reconcileVideoProcessingQueue(ctx context.Context, db *sql.DB, client *asynq.Client) {
	enqueuePending := func() {
		for offset := 0; ; offset += 100 {
			rows, err := db.QueryContext(ctx, `SELECT video_id FROM videos WHERE status=$1 AND delete_flag=0 ORDER BY upload_end_time NULLS FIRST,video_id LIMIT 100 OFFSET $2`, types.UploadedOnServer, offset)
			if err != nil {
				logger.Log.Error("failed to find videos needing processing enqueue", "error", err)
				return
			}
			count := 0
			for rows.Next() {
				count++
				var videoID string
				if err := rows.Scan(&videoID); err != nil {
					logger.Log.Error("failed to read video needing processing enqueue", "error", err)
					continue
				}
				task, err := queueing.NewProcessVideoTask(videoID)
				if err == nil {
					_, err = client.EnqueueContext(ctx, task, queueing.ProcessVideoTaskOptions(
						config.AppConfig.VideoTaskMaxRetries,
						config.AppConfig.VideoTaskTimeout,
						config.AppConfig.VideoTaskRetention,
						config.AppConfig.VideoTaskUniqueTTL,
					)...)
				}
				if err != nil && !errors.Is(err, asynq.ErrDuplicateTask) {
					logger.Log.Error("failed to enqueue pending video", "video_id", videoID, "error", err)
				}
			}
			rowsErr := rows.Err()
			_ = rows.Close()
			if rowsErr != nil {
				logger.Log.Error("failed to iterate videos needing processing enqueue", "error", rowsErr)
				return
			}
			if count < 100 {
				return
			}
		}
	}
	enqueuePending()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			enqueuePending()
		}
	}
}
