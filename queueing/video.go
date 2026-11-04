package queueing

import (
	"encoding/json"
	"time"

	"github.com/hibiken/asynq"
)

const (
	TypeProcessVideo = "video:process:v1"
)

type ProcessVideoPayload struct {
	VideoID string `json:"video_id"`
}

func NewProcessVideoTask(videoID string) (*asynq.Task, error) {
	payload, err := json.Marshal(ProcessVideoPayload{VideoID: videoID})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeProcessVideo, payload), nil
}

func ProcessVideoTaskOptions(maxRetries int, timeout, retention, uniqueTTL time.Duration) []asynq.Option {
	options := []asynq.Option{
		asynq.MaxRetry(maxRetries),
		asynq.Timeout(timeout),
		asynq.Retention(retention),
	}
	if uniqueTTL > 0 {
		options = append(options, asynq.Unique(uniqueTTL))
	}
	return options
}
