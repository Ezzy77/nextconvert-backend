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

	// Database: PostgreSQL connection string.
	// Local: postgres://postgres:postgres@localhost:5432/convert_studio?sslmode=disable
	// Supabase (direct): postgresql://postgres:[PASSWORD]@db.[PROJECT-REF].supabase.co:5432/postgres?sslmode=require
	DatabaseURL string
	RedisURL    string

	// Storage
	Storage StorageConfig

	// FFmpeg
	FFmpegPath          string
	FFprobePath         string
	FFmpegMaxThreads    int  // Max CPU threads for FFmpeg (0 = unlimited)
	FFmpegHardwareAccel bool // Use hardware acceleration (VideoToolbox on macOS)
	FFmpegFastPresets   bool // Use faster encoding presets (less CPU, slightly larger files)

	// Worker
	WorkerConcurrency int

	// Security & Authentication (Clerk)
	ClerkSecretKey string
	AllowedOrigins []string

	// Limits
	MaxUploadSize  int64
	MaxJobsPerUser int

	// Stripe
	StripeSecretKey       string
	StripeWebhookSecret   string
	StripeBasicPriceID    string
	StripeStandardPriceID string
	StripeProPriceID      string
	StripeSuccessURL      string
	StripeCancelURL       string
}

// StorageConfig holds storage-specific configuration
type StorageConfig struct {
	Backend     string // local, s3, supabase
	BasePath    string
	S3Endpoint  string
	S3Bucket    string
	S3AccessKey string
	S3SecretKey string
	S3Region    string
	// Supabase Storage (backend=supabase)
	SupabaseURL        string // https://PROJECT_REF.supabase.co
	SupabaseServiceKey string // service_role key for server-side ops (bypasses RLS)
	SupabaseBucket     string // bucket name (default: media)
	SupabaseTimeout    int    // HTTP timeout in seconds (default: 900 = 15 min for large uploads)
}

// Load reads configuration from environment variables
func Load() (*Config, error) {
	// Load .env file if it exists
	godotenv.Load()

	cfg := &Config{
		Environment:         getEnv("ENVIRONMENT", "development"),
		Port:                getEnvInt("PORT", 8080),
		LogLevel:            getEnv("LOG_LEVEL", "info"),
		DatabaseURL:         getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/convert_studio?sslmode=disable"),
		RedisURL:            getEnv("REDIS_URL", "localhost:6379"),
		FFmpegPath:          getEnv("FFMPEG_PATH", "ffmpeg"),
		FFprobePath:         getEnv("FFPROBE_PATH", "ffprobe"),
		FFmpegMaxThreads:    getEnvInt("FFMPEG_MAX_THREADS", 0),         // Default: 0 = auto (use available cores)
		FFmpegHardwareAccel: getEnvBool("FFMPEG_HARDWARE_ACCEL", false), // Default: false (cloud servers typically don't have GPU)
		FFmpegFastPresets:   getEnvBool("FFMPEG_FAST_PRESETS", true),    // Default: use fast presets for quicker processing
		WorkerConcurrency:   getEnvInt("WORKER_CONCURRENCY", 2),
		ClerkSecretKey:      getEnv("CLERK_SECRET_KEY", ""),
		AllowedOrigins:      []string{getEnv("ALLOWED_ORIGINS", "http://localhost:5173")},
		MaxUploadSize:       getEnvInt64("MAX_UPLOAD_SIZE", 5*1024*1024*1024), // 5GB
		MaxJobsPerUser:      getEnvInt("MAX_JOBS_PER_USER", 20),
		StripeSecretKey:     getEnv("STRIPE_SECRET_KEY", ""),
		StripeWebhookSecret: getEnv("STRIPE_WEBHOOK_SECRET", ""),
		StripeBasicPriceID:  getEnv("STRIPE_BASIC_PRICE_ID", ""),
		StripeStandardPriceID: getEnv("STRIPE_STANDARD_PRICE_ID", ""),
		StripeProPriceID:    getEnv("STRIPE_PRO_PRICE_ID", ""),
		StripeSuccessURL:    getEnv("STRIPE_SUCCESS_URL", "http://localhost:5173/pricing?success=true"),
		StripeCancelURL:     getEnv("STRIPE_CANCEL_URL", "http://localhost:5173/pricing"),
		Storage: StorageConfig{
			Backend:            getEnv("STORAGE_BACKEND", "local"),
			BasePath:           getEnv("STORAGE_BASE_PATH", "./data"),
			S3Endpoint:         getEnv("S3_ENDPOINT", ""),
			S3Bucket:           getEnv("S3_BUCKET", ""),
			S3AccessKey:        getEnv("S3_ACCESS_KEY", ""),
			S3SecretKey:        getEnv("S3_SECRET_KEY", ""),
			S3Region:           getEnv("S3_REGION", "us-east-1"),
			SupabaseURL:        getEnv("SUPABASE_URL", ""),
			SupabaseServiceKey: getEnv("SUPABASE_SERVICE_ROLE_KEY", ""),
			SupabaseBucket:     getEnv("SUPABASE_STORAGE_BUCKET", "media"),
			SupabaseTimeout:    getEnvInt("SUPABASE_STORAGE_TIMEOUT", 900),
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

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return value == "true" || value == "1" || value == "yes"
	}
	return defaultValue
}
