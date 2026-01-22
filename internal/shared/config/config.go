package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds all application configuration
type Config struct {
	// Server
	Environment string
	Port        int
	LogLevel    string

	// Database
	DatabaseURL string
	RedisURL    string

	// Storage
	Storage StorageConfig

	// External tools
	FFmpegPath string
	PandocPath string

	// Worker
	WorkerConcurrency int

	// Security
	JWTSecret     string
	AllowedOrigins []string

	// Limits
	MaxUploadSize int64
	MaxJobsPerUser int
}

// StorageConfig holds storage-specific configuration
type StorageConfig struct {
	Backend     string // local, s3, gcs
	BasePath    string
	S3Endpoint  string
	S3Bucket    string
	S3AccessKey string
	S3SecretKey string
	S3Region    string
}

// Load reads configuration from environment variables
func Load() (*Config, error) {
	// Load .env file if it exists
	godotenv.Load()

	cfg := &Config{
		Environment:       getEnv("ENVIRONMENT", "development"),
		Port:              getEnvInt("PORT", 8080),
		LogLevel:          getEnv("LOG_LEVEL", "info"),
		DatabaseURL:       getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/convert_studio?sslmode=disable"),
		RedisURL:          getEnv("REDIS_URL", "localhost:6379"),
		FFmpegPath:        getEnv("FFMPEG_PATH", "ffmpeg"),
		PandocPath:        getEnv("PANDOC_PATH", "pandoc"),
		WorkerConcurrency: getEnvInt("WORKER_CONCURRENCY", 2),
		JWTSecret:         getEnv("JWT_SECRET", "change-me-in-production"),
		AllowedOrigins:    []string{getEnv("ALLOWED_ORIGINS", "http://localhost:5173")},
		MaxUploadSize:     getEnvInt64("MAX_UPLOAD_SIZE", 5*1024*1024*1024), // 5GB
		MaxJobsPerUser:    getEnvInt("MAX_JOBS_PER_USER", 20),
		Storage: StorageConfig{
			Backend:     getEnv("STORAGE_BACKEND", "local"),
			BasePath:    getEnv("STORAGE_BASE_PATH", "./data"),
			S3Endpoint:  getEnv("S3_ENDPOINT", ""),
			S3Bucket:    getEnv("S3_BUCKET", ""),
			S3AccessKey: getEnv("S3_ACCESS_KEY", ""),
			S3SecretKey: getEnv("S3_SECRET_KEY", ""),
			S3Region:    getEnv("S3_REGION", "us-east-1"),
		},
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
			return intVal
		}
	}
	return defaultValue
}
