// Package redisconn builds the Redis client options shared by the API, worker,
// and Asynqmon services.
package redisconn

import (
	"fmt"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"video-streaming-server/config"
)

func GoRedisOptions() (*redis.Options, error) {
	if config.AppConfig.RedisURL != "" {
		options, err := redis.ParseURL(config.AppConfig.RedisURL)
		if err != nil {
			return nil, fmt.Errorf("parse REDIS_URL: %w", err)
		}
		return options, nil
	}
	return &redis.Options{
		Addr:     config.AppConfig.RedisAddr,
		Password: config.AppConfig.RedisPassword,
		DB:       config.AppConfig.RedisDB,
	}, nil
}

func AsynqOptions() (asynq.RedisConnOpt, error) {
	if config.AppConfig.RedisURL != "" {
		options, err := asynq.ParseRedisURI(config.AppConfig.RedisURL)
		if err != nil {
			return nil, fmt.Errorf("parse REDIS_URL for Asynq: %w", err)
		}
		return options, nil
	}
	return asynq.RedisClientOpt{
		Addr:     config.AppConfig.RedisAddr,
		Password: config.AppConfig.RedisPassword,
		DB:       config.AppConfig.RedisDB,
	}, nil
}
