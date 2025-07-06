package types

import (
	"database/sql"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type HomePageData struct {
	IsLoggedIn bool
	Username   string
}

type UpdateRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type Stream struct {
	CodecName string `json:"codec_name"`
	CodecType string `json:"codec_type"`
}

type Format struct {
	Filename string `json:"filename"`
	Duration string `json:"duration"`
	BitRate  string `json:"bit_rate"`
	Size     string `json:"size"`
}

type FFProbeOutput struct {
	Streams []Stream `json:"streams"`
	Format  Format   `json:"format"`
}

type Video struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Thumbnail   sql.NullString `json:"thumbnail"`
	Status      VideoStatus    `json:"status"`
}

type SessionID string
type UserID string

type SessionSSEChannelMap struct {
	Sessions map[SessionID]SSEChannel `json:"Sessions"`
}

type SSEChannel struct {
	OriginatingPage string       `json:"originating_page"`
	EventChannel    chan SSEType `json:"-"`
}

type User struct {
	ID             string    `json:"id"`
	Username       string    `json:"username"`
	Email          string    `json:"email"`
	HashedPassword []byte    `json:"-"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type ThumbnailUploadResponse struct {
	ID       string `json:"$id"`
	BucketID string `json:"bucketId"`
}

type VideoResponseType struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Thumbnail   string      `json:"thumbnail"`
	Status      VideoStatus `json:"status"`
}

func NewUser(username, email, password string) (*User, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	return &User{
		Username:       username,
		Email:          email,
		HashedPassword: hashedPassword,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}, nil
}

func (u *User) SetPassword(password string) error {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.HashedPassword = hashedPassword
	return nil
}

func (u *User) ComparePassword(password string) bool {
	return bcrypt.CompareHashAndPassword(u.HashedPassword, []byte(password)) == nil
}

func (u *User) UpdateInfo(newUsername, newEmail string) {
	u.Username = newUsername
	u.Email = newEmail
	u.UpdatedAt = time.Now()
}

func (u *User) GetID() string {
	return u.ID
}

func (u *User) GetUsername() string {
	return u.Username
}

func (u *User) GetEmail() string {
	return u.Email
}

type VideoStatus int

const (
	ProcessingFailed    VideoStatus = -1
	UploadPending       VideoStatus = 0
	UploadedOnServer    VideoStatus = 1
	ProcessingCompleted VideoStatus = 2
)

type SSEType struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}

type EmailPayload struct {
	Sender     string
	Recipients []string
	IsHTML     bool
	Subject    string
	Body       string
}
