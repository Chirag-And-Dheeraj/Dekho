package main

import (
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"time"
	"video-streaming-server/config"
	"video-streaming-server/controllers"
	"video-streaming-server/database"
	mw "video-streaming-server/middleware"
	"video-streaming-server/repositories"
	"video-streaming-server/services"
	"video-streaming-server/shared"
	"video-streaming-server/shared/logger"
	"video-streaming-server/sse"
	"video-streaming-server/types"
	"video-streaming-server/utils"
)

/*
/video/ - Get All Videos
/video/[id] - Get A Video
/video/[id]/stream - Get The Manifest File For The Video
/video/[id]/stream/[filename] - Get The Segment of Video
*/

func videoHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	method := r.Method
	db, err := database.GetDBConn()

	if err != nil {
		logger.Log.Error("failed to get database connection", "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	if method == http.MethodPost {
		if matched, _ := regexp.MatchString(`^/video/[a-zA-Z0-9-]+/complete/?$`, path); matched {
			controllers.CompleteVideoUpload(w, r, db)
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
		} else if matched, err := regexp.MatchString("^/video/[a-zA-B0-9-]+/stream/?$", path); err == nil && matched {
			controllers.ManifestFileHandler(w, r, db)
		} else if matched, err := regexp.MatchString("^/video/[a-zA-B0-9-]+/stream/[a-zA-B0-9_-]+.ts/?$", r.URL.Path); err == nil && matched {
			controllers.TSFileHandler(w, r, db)
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

	utils.PrettyPrintMap(shared.GlobalUserSSEConnectionsMap, "GlobalUserSSEConnectionsMap")

	logger.Log.Info("user connected to the event stream", "user id", userID, "session id", sessionID)

	disconnected := r.Context().Done()
	rc := http.NewResponseController(w)

	// the intent behind this flush is for an initial connection heartbeat
	rc.Flush()

	channel := shared.GlobalUserSSEConnectionsMap[userID].Sessions[sessionID].EventChannel

	for {
		select {
		case <-disconnected:
			logger.Log.Info("user disconnected from event stream", "user id", userID, "session id", sessionID)
			utils.PrettyPrintMap(shared.GlobalUserSSEConnectionsMap, "GlobalUserSSEConnectionsMap")
			return
		case rawMessage := <-channel:

			message := rawMessage
			event := message.Event
			rawData := message.Data
			eventData, err := json.Marshal(rawData)
			if err != nil {
				logger.Log.Error("failed to marshal event data", "error", err)
				continue
			}
			logger.Log.Debug("sending event data to user on session", "event", event, "event data", eventData, "user id", userID, "session id", sessionID)
			if n, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, eventData); err != nil {
				logger.Log.Error("failed to send event data to user on session", "event", event, "event data", eventData, "user id", userID, "session id", sessionID, "error", err)
				utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
			} else {
				logger.Log.Info("sent event data to user on session", "event", event, "event data", eventData, "user id", userID, "session id", sessionID, "bytes written", n)
			}

			rc.Flush()
		}
	}
}

func setUpRoutes() {
	logger.Log.Debug("setting up routes")
	http.HandleFunc("/", utils.Chain(homePageHandler, mw.Logging))
	http.HandleFunc("/register", utils.Chain(registerPageHandler, mw.Logging))
	http.HandleFunc("/login", utils.Chain(loginPageHandler, mw.Logging))
	http.HandleFunc("/logout", utils.Chain(logoutHandler, mw.Logging))
	http.HandleFunc("/upload", utils.Chain(uploadPageHandler, mw.Logging, mw.AuthRequired))
	http.HandleFunc("/list", utils.Chain(listPageHandler, mw.Logging, mw.AuthRequired))
	http.HandleFunc("/watch", utils.Chain(watchPageHandler, mw.Logging, mw.AuthRequired))
	http.HandleFunc("/config", configHandler)
	http.HandleFunc("/video/", utils.Chain(videoHandler, mw.Logging, mw.AuthRequired))
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
	setUpRoutes()
	logger.Log.Info(
		"Dekho server is listening on",
		"address", config.AppConfig.Addr,
		"port", config.AppConfig.Port,
	)
	err := http.ListenAndServe(fmt.Sprintf("%s:%s", config.AppConfig.Addr, config.AppConfig.Port), nil)
	if err != nil {
		slog.Error("server failed to start", "error", err)
		os.Exit(1)
	}
}
