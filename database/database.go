package database

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"video-streaming-server/config"
	"video-streaming-server/shared/logger"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var DB *sql.DB

// DBConfig holds the database connection details
type DBConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
	SSLMode  string
	URL      string
}

// NewDBConfig creates a new DBConfig from environment variables
func newDBConfig() (*DBConfig, error) {
	return &DBConfig{
		Host:     config.AppConfig.DBHost,
		Port:     config.AppConfig.DBPort,
		User:     config.AppConfig.DBUser,
		Password: config.AppConfig.DBPassword,
		Name:     config.AppConfig.DBName,
		SSLMode:  config.AppConfig.SSLMode,
		URL:      config.AppConfig.DatabaseURL,
	}, nil
}

// Connect establishes a connection to the PostgreSQL database
func Connect(config *DBConfig) (*sql.DB, error) {
	logger.Log.Debug("initializing PostgreSQL database")

	psqlInfo := config.URL
	if psqlInfo == "" {
		psqlInfo = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
			config.Host, config.Port, config.User, config.Password, config.Name, config.SSLMode)
	}

	db, err := sql.Open("pgx", psqlInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	err = db.Ping()
	if err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	logger.Log.Info("database initialized")
	return db, nil
}

// ListenerConnectionString removes pgx-specific channel_binding settings for
// lib/pq's LISTEN connection while retaining TLS requirements.
func ListenerConnectionString() string {
	connectionString := ConnectionString()
	if !strings.Contains(connectionString, "://") {
		return connectionString
	}
	parsed, err := url.Parse(connectionString)
	if err != nil {
		return connectionString
	}
	query := parsed.Query()
	query.Del("channel_binding")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func GetDBConn() (*sql.DB, error) {
	if DB == nil {
		dbConfig, err := newDBConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create database config: %w", err)
		}
		DB, err = Connect(dbConfig)

		if err != nil {
			return nil, fmt.Errorf("failed to connect to database: %w", err)
		}
	}
	return DB, nil
}

func ConnectionString() string {
	dbConfig, _ := newDBConfig()
	if dbConfig.URL != "" {
		return dbConfig.URL
	}
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		dbConfig.Host, dbConfig.Port, dbConfig.User, dbConfig.Password, dbConfig.Name, dbConfig.SSLMode)
}
