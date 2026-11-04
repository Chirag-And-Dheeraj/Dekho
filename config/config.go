package config

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type FileType struct {
	FileType      string `json:"file_type"`
	FileExtension string `json:"file_extension"`
}
type ConfigResponse struct {
	FileSizeLimit      string     `json:"file_size_limit"`
	SupportedFileTypes []FileType `json:"supported_file_types"`
}

type Config struct {
	RootPath               string
	AppwriteBucketID       string
	AppwriteProjectID      string
	AppwriteKey            string
	AppwriteResponseFormat string
	AppwriteS3Endpoint     string
	AppwriteS3Region       string
	DBHost                 string
	DBPort                 string
	DBUser                 string
	DBPassword             string
	DBName                 string
	RedisAddr              string
	RedisPassword          string
	RedisDB                int
	VideoTaskMaxRetries    int
	VideoTaskTimeout       time.Duration
	VideoTaskRetention     time.Duration
	VideoTaskUniqueTTL     time.Duration
	WorkerConcurrency      int
	WorkerShutdownTimeout  time.Duration
	Port                   string
	Addr                   string
	SSLMode                string
	JWTSecretKey           string
	FileSizeLimit          string
	Debug                  bool

	SMTPHost     string
	SMTPPort     string
	SMTPPassword string
	SMTPUser     string
}

var AppConfig *Config

func LoadEnvFile(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("error reading %s: %w", filename, err)
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			return fmt.Errorf("error reading invalid environment variable definition: %s", line)
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, value)
		}
		slog.Debug("Loaded environment variable", "key", key, "value", value)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading %s: %w", filename, err)
	}

	return nil
}

func LoadConfig(envFile string) error {
	if err := LoadEnvFile(envFile); err != nil {
		return err
	}

	debug, err := strconv.ParseBool(os.Getenv("DEBUG"))
	if err != nil {
		return fmt.Errorf("error parsing DEBUG environment variable: %w", err)
	}
	redisDB := 0
	if value := os.Getenv("REDIS_DB"); value != "" {
		redisDB, err = strconv.Atoi(value)
		if err != nil || redisDB < 0 {
			return fmt.Errorf("error parsing REDIS_DB environment variable")
		}
	}
	workerConcurrency := 2
	if value := os.Getenv("WORKER_CONCURRENCY"); value != "" {
		workerConcurrency, err = strconv.Atoi(value)
		if err != nil || workerConcurrency < 1 {
			return fmt.Errorf("error parsing WORKER_CONCURRENCY environment variable")
		}
	}
	videoTaskMaxRetries := 5
	if value := os.Getenv("VIDEO_TASK_MAX_RETRIES"); value != "" {
		videoTaskMaxRetries, err = strconv.Atoi(value)
		if err != nil || videoTaskMaxRetries < 0 {
			return fmt.Errorf("error parsing VIDEO_TASK_MAX_RETRIES environment variable")
		}
	}
	videoTaskTimeout, err := parseDurationEnv("VIDEO_TASK_TIMEOUT", 6*time.Hour, false)
	if err != nil {
		return err
	}
	videoTaskRetention, err := parseDurationEnv("VIDEO_TASK_RETENTION", 24*time.Hour, true)
	if err != nil {
		return err
	}
	videoTaskUniqueTTL, err := parseDurationEnv("VIDEO_TASK_UNIQUE_TTL", 24*time.Hour, true)
	if err != nil {
		return err
	}
	if videoTaskUniqueTTL > 0 && videoTaskUniqueTTL < time.Second {
		return fmt.Errorf("VIDEO_TASK_UNIQUE_TTL must be at least 1s when enabled")
	}
	workerShutdownTimeout, err := parseDurationEnv("WORKER_SHUTDOWN_TIMEOUT", 2*time.Minute, false)
	if err != nil {
		return err
	}

	config := &Config{
		RootPath:               os.Getenv("ROOT_PATH"),
		AppwriteBucketID:       os.Getenv("BUCKET_ID"),
		AppwriteProjectID:      os.Getenv("APPWRITE_PROJECT_ID"),
		AppwriteKey:            os.Getenv("APPWRITE_KEY"),
		AppwriteResponseFormat: os.Getenv("APPWRITE_RESPONSE_FORMAT"),
		AppwriteS3Endpoint:     os.Getenv("APPWRITE_S3_ENDPOINT"),
		AppwriteS3Region:       os.Getenv("APPWRITE_S3_REGION"),
		DBHost:                 os.Getenv("DB_HOST"),
		DBPort:                 os.Getenv("DB_PORT"),
		DBUser:                 os.Getenv("DB_USER"),
		DBPassword:             os.Getenv("DB_PASSWORD"),
		DBName:                 os.Getenv("DB_NAME"),
		RedisAddr:              os.Getenv("REDIS_ADDR"),
		RedisPassword:          os.Getenv("REDIS_PASSWORD"),
		RedisDB:                redisDB,
		VideoTaskMaxRetries:    videoTaskMaxRetries,
		VideoTaskTimeout:       videoTaskTimeout,
		VideoTaskRetention:     videoTaskRetention,
		VideoTaskUniqueTTL:     videoTaskUniqueTTL,
		WorkerConcurrency:      workerConcurrency,
		WorkerShutdownTimeout:  workerShutdownTimeout,
		Port:                   os.Getenv("PORT"),
		Addr:                   os.Getenv("ADDR"),
		SSLMode:                os.Getenv("SSL_MODE"),
		JWTSecretKey:           os.Getenv("JWT_SECRET_KEY"),
		FileSizeLimit:          os.Getenv("FILE_SIZE_LIMIT"),
		Debug:                  debug,

		SMTPHost:     os.Getenv("SMTPHost"),
		SMTPPort:     os.Getenv("SMTPPort"),
		SMTPPassword: os.Getenv("SMTPPassword"),
		SMTPUser:     os.Getenv("SMTPUser"),
	}
	if config.RedisAddr == "" {
		config.RedisAddr = "localhost:6379"
	}
	if config.AppwriteS3Region == "" {
		config.AppwriteS3Region = "auto"
	}

	if config.JWTSecretKey == "" {
		bytes := make([]byte, 32)
		_, _ = rand.Read(bytes)
		config.JWTSecretKey = base64.StdEncoding.EncodeToString(bytes)
		os.Setenv("JWT_SECRET_KEY", config.JWTSecretKey)
	}

	AppConfig = config
	slog.Info("Configuration loaded successfully")
	return nil
}

func parseDurationEnv(name string, fallback time.Duration, allowZero bool) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("error parsing %s environment variable: %w", name, err)
	}
	if duration < 0 {
		return 0, fmt.Errorf("%s must not be negative", name)
	}
	if duration == 0 && !allowZero {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}
	return duration, nil
}
