package middleware

import (
	"net/http"
	"video-streaming-server/shared/logger"
	"video-streaming-server/utils"

	"github.com/golang-jwt/jwt/v5"
)

func AuthRequired(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("auth_token")
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		tokenString := cookie.Value
		token, err := utils.VerifyToken(tokenString)
		if err != nil || !token.Valid {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		next.ServeHTTP(w, r)
	}
}

func Logging(next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := "anonymous"
		if cookie, err := r.Cookie("auth_token"); err == nil {
			if token, err := utils.VerifyToken(cookie.Value); err == nil && token.Valid {
				if claims, ok := token.Claims.(jwt.MapClaims); ok {
					if id, ok := claims["user_id"].(string); ok {
						userID = id
					}
				}
			}
		}

		logger.Log.Info("http request",
			"method", r.Method,
			"url", r.URL.String(),
			"user", userID,
		)
		next.ServeHTTP(w, r)
	})
}
