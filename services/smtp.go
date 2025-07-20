package services

import (
	"errors"
	"net/smtp"
	"strings"
	"video-streaming-server/config"
	"video-streaming-server/shared/logger"
	"video-streaming-server/types"
)

// SendEmail is a wrapper over smtp.SendEmail.
//
// This function consumes information like host address, username and password from the config.
//
// Recipients is an array of string which is joined using comma (,)
// separator in the function, creating a comma separated string
func SendEmail(msg *types.EmailPayload) error {
	if len(msg.Recipients) == 0 {
		return errors.New("Recipients should not be emtpy")
	}

	user := config.AppConfig.SMTPUser
	password := config.AppConfig.SMTPPassword

	host := config.AppConfig.SMTPHost
	port := config.AppConfig.SMTPPort
	addr := host + ":" + port

	auth := smtp.PlainAuth("", user, password, host)

	message := "From: " + msg.Sender + "\r\n" +
		"To: " + strings.Join(msg.Recipients, ",") + "\r\n" +
		"Subject: " + msg.Subject + "\r\n"

	if msg.IsHTML {
		message += "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\n\n"
	}

	message += msg.Body

	err := smtp.SendMail(addr, auth, user, msg.Recipients, []byte(message))

	if err != nil {
		logger.Log.Error(err.Error())

		return err
	}

	logger.Log.Info("email sent to recipients", "recipients", msg.Recipients)

	return nil
}
