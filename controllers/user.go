package controllers

import (
	"encoding/json"
	"net/http"
	"time"
	"video-streaming-server/config"
	"video-streaming-server/services"
	"video-streaming-server/shared/logger"
	"video-streaming-server/types"
	"video-streaming-server/utils"

	"github.com/go-playground/validator"
)

type RegisterRequest struct {
	Username        string `json:"username" validate:"required,min=3,max=32"`
	Email           string `json:"email" validate:"required,email"`
	Password        string `json:"password" validate:"required,min=8"`
	ConfirmPassword string `json:"confirm_password" validate:"required,eqfield=Password"`
}

type RegisterResponse struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

func RegisterUser(w http.ResponseWriter, r *http.Request, userService services.UserService) {
	var requestBody RegisterRequest

	err := json.NewDecoder(r.Body).Decode(&requestBody)
	if err != nil {
		logger.Log.Error("failed to decode request body", "error", err)
		utils.SendError(w, http.StatusBadRequest, "Invalid Request Body")
		return
	}

	validate := validator.New()
	err = validate.Struct(requestBody)
	if err != nil {
		logger.Log.Error("validation failed", "error", err)
		utils.SendError(w, http.StatusBadRequest, "Validation failed")
		return
	}

	newUser, err := userService.RegisterUser(requestBody.Username, requestBody.Email, requestBody.Password)
	if err != nil {
		switch err.Error() {
		case "email already exists":
			logger.Log.Info("registration failed - email exists", "email", requestBody.Email)
			utils.SendError(w, http.StatusConflict, "Email already exists")
		case "username already exists":
			logger.Log.Info("registration failed - username exists", "username", requestBody.Username)
			utils.SendError(w, http.StatusConflict, "Username already taken")
		default:
			logger.Log.Error("registration failed - internal error", "error", err)
			utils.SendError(w, http.StatusInternalServerError, "Internal server error")
		}
		return
	}

	verificationLink, err := utils.GenerateVerificationLink(newUser.Email)

	err = services.SendEmail(&types.EmailPayload{
		IsHTML: false,
		Sender: config.AppConfig.SMTPUser,
		Subject: "Verify Dekho account",
		Recipients: []string{newUser.Email},
		Body: "To get access to our services, please verify your account using link " + verificationLink,
	})

	logger.Log.Info("user registered successfully",
		"userId", newUser.ID)

	response := RegisterResponse{
		ID:        newUser.ID,
		Username:  newUser.Username,
		Email:     newUser.Email,
		CreatedAt: newUser.CreatedAt,
	}

	jsonResponse, err := json.Marshal(response)
	if err != nil {
		logger.Log.Error("failed to marshal response", "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	w.Write(jsonResponse)
}

func LoginUser(w http.ResponseWriter, r *http.Request, userService services.UserService) {
	var requestBody LoginRequest

	err := json.NewDecoder(r.Body).Decode(&requestBody)
	if err != nil {
		logger.Log.Error("failed to decode request body", "error", err)
		utils.SendError(w, http.StatusBadRequest, "Invalid Request Body")
		return
	}

	validate := validator.New()
	err = validate.Struct(requestBody)
	if err != nil {
		logger.Log.Error("validation failed", "error", err)
		utils.SendError(w, http.StatusBadRequest, "Invalid Request Body")
		return
	}

	user, err := userService.AuthenticateUser(requestBody.Email, requestBody.Password)
	if err != nil {
		switch err.Error() {
		case "invalid credentials":
			logger.Log.Info("login failed - invalid credentials",
				"email", requestBody.Email,
				"error", err.Error())
			utils.SendError(w, http.StatusUnauthorized, "Invalid email or password")
		case "user does not exist":
			logger.Log.Info("login failed - user does not exist",
				"email", requestBody.Email,
				"error", err.Error())
			utils.SendError(w, http.StatusNotFound, "User does not exist")
		default:
			logger.Log.Error("login failed - internal error", "error", err)
			utils.SendError(w, http.StatusInternalServerError, "Internal server error")
		}
		return
	}

	// Generate JWT token
	token, err := utils.GenerateJWT(user.ID, user.Username)
	if err != nil {
		logger.Log.Error("failed to generate JWT", "error", err)
		utils.SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	logger.Log.Info("user logged in successfully",
		"userId", user.ID,
		"username", user.Username)

	// Set the JWT token in the response
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
		MaxAge:   86400, // 24 hours
	})

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"message": "Logged in successfully"}`))
}
