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
	Filename   string `json:"filename"`
	FormatName string `json:"format_name"`
	Duration   string `json:"duration"`
	BitRate    string `json:"bit_rate"`
	Size       string `json:"size"`
}

type VideoSource string

const (
	VideoSourceUploadForm VideoSource = "upload_form"
	VideoSourceURLImport  VideoSource = "url_import"
	VideoSourceAPIImport  VideoSource = "api_import"
	VideoSourceOther      VideoSource = "other"
)

type VideoCodec string

const (
	VideoCodecH264    VideoCodec = "h264"
	VideoCodecHEVC    VideoCodec = "hevc"
	VideoCodecVP9     VideoCodec = "vp9"
	VideoCodecAV1     VideoCodec = "av1"
	VideoCodecMPEG4   VideoCodec = "mpeg4"
	VideoCodecMPEG2   VideoCodec = "mpeg2video"
	VideoCodecOther   VideoCodec = "other"
	VideoCodecUnknown VideoCodec = "unknown"
	VideoCodecNone    VideoCodec = "none"
)

type AudioCodec string

const (
	AudioCodecAAC     AudioCodec = "aac"
	AudioCodecMP3     AudioCodec = "mp3"
	AudioCodecOpus    AudioCodec = "opus"
	AudioCodecVorbis  AudioCodec = "vorbis"
	AudioCodecAC3     AudioCodec = "ac3"
	AudioCodecEAC3    AudioCodec = "eac3"
	AudioCodecPCM     AudioCodec = "pcm"
	AudioCodecOther   AudioCodec = "other"
	AudioCodecUnknown AudioCodec = "unknown"
	AudioCodecNone    AudioCodec = "none"
)

type VideoContainer string

const (
	VideoContainerMP4      VideoContainer = "mp4"
	VideoContainerMatroska VideoContainer = "matroska"
	VideoContainerMOV      VideoContainer = "mov"
	VideoContainerWebM     VideoContainer = "webm"
	VideoContainerMPEGTS   VideoContainer = "mpegts"
	VideoContainerHLS      VideoContainer = "hls"
	VideoContainerOther    VideoContainer = "other"
	VideoContainerUnknown  VideoContainer = "unknown"
)

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
	ID                 string         `json:"id"`
	Title              string         `json:"title"`
	Description        string         `json:"description"`
	Thumbnail          string         `json:"thumbnail"`
	Status             VideoStatus    `json:"status"`
	DurationSeconds    *float64       `json:"duration_seconds,omitempty"`
	OriginalVideoCodec VideoCodec     `json:"original_video_codec,omitempty"`
	OriginalAudioCodec AudioCodec     `json:"original_audio_codec,omitempty"`
	OriginalContainer  VideoContainer `json:"original_container,omitempty"`
	FinalVideoCodec    VideoCodec     `json:"final_video_codec,omitempty"`
	FinalAudioCodec    AudioCodec     `json:"final_audio_codec,omitempty"`
	FinalContainer     VideoContainer `json:"final_container,omitempty"`
	VideoSource        VideoSource    `json:"video_source,omitempty"`
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
	ID    string `json:"id,omitempty"`
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
