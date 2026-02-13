package middleware

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// RateLimiter implements token bucket rate limiting using Redis
type RateLimiter struct {
	redis  *redis.Client
	logger *zap.Logger
}

// RateLimitConfig defines rate limit rules
type RateLimitConfig struct {
	Requests int           // Number of requests allowed
	Window   time.Duration // Time window
	KeyFunc  func(*http.Request) string // Function to generate rate limit key
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(redis *redis.Client, logger *zap.Logger) *RateLimiter {
	return &RateLimiter{
		redis:  redis,
		logger: logger,
	}
}

// Limit returns a middleware that enforces rate limiting
func (rl *RateLimiter) Limit(config RateLimitConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			
			// Generate rate limit key
			key := config.KeyFunc(r)
			if key == "" {
				rl.logger.Warn("Rate limit key is empty, allowing request")
				next.ServeHTTP(w, r)
				return
			}
			
			// Check rate limit
			allowed, remaining, resetTime, err := rl.checkLimit(ctx, key, config)
			if err != nil {
				rl.logger.Error("Rate limit check failed", zap.Error(err))
				// On error, allow request (fail open)
				next.ServeHTTP(w, r)
				return
			}
			
			// Set rate limit headers
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(config.Requests))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetTime.Unix(), 10))
			
			if !allowed {
				w.Header().Set("Retry-After", strconv.FormatInt(int64(time.Until(resetTime).Seconds()), 10))
				http.Error(w, "Rate limit exceeded. Please try again later.", http.StatusTooManyRequests)
				
				rl.logger.Warn("Rate limit exceeded",
					zap.String("key", key),
					zap.String("path", r.URL.Path),
				)
				return
			}
			
			next.ServeHTTP(w, r)
		})
	}
}

// checkLimit checks if the request is within rate limit
func (rl *RateLimiter) checkLimit(ctx context.Context, key string, config RateLimitConfig) (bool, int, time.Time, error) {
	now := time.Now()
	window := config.Window
	
	// Redis key with window identifier
	redisKey := fmt.Sprintf("ratelimit:%s:%d", key, now.Unix()/int64(window.Seconds()))
	
	// Increment counter
	pipe := rl.redis.Pipeline()
	incr := pipe.Incr(ctx, redisKey)
	pipe.Expire(ctx, redisKey, window)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, 0, time.Time{}, err
	}
	
	count := int(incr.Val())
	remaining := config.Requests - count
	if remaining < 0 {
		remaining = 0
	}
	
	// Calculate reset time (end of current window)
	resetTime := now.Add(window)
	
	allowed := count <= config.Requests
	return allowed, remaining, resetTime, nil
}

// GetRealIP extracts the real client IP address from the request
// It checks proxy headers in order: X-Forwarded-For, X-Real-IP, RemoteAddr
func GetRealIP(r *http.Request) string {
	// Check X-Forwarded-For header (can contain multiple IPs)
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// X-Forwarded-For can be: "client, proxy1, proxy2"
		// We want the first (original client) IP
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			clientIP := strings.TrimSpace(ips[0])
			if clientIP != "" {
				return clientIP
			}
		}
	}
	
	// Check X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return strings.TrimSpace(xri)
	}
	
	// Fall back to RemoteAddr (format: "IP:port")
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr doesn't have a port, use as-is
		return r.RemoteAddr
	}
	return ip
}

// Common key functions

// KeyByIP generates rate limit key based on IP address
func KeyByIP(r *http.Request) string {
	ip := GetRealIP(r)
	return fmt.Sprintf("ip:%s", ip)
}

// KeyByUser generates rate limit key based on user ID from context.
// Falls back to IP if no user is in context.
func KeyByUser(r *http.Request) string {
	user := GetUser(r.Context())
	if user != nil && user.ID != "" {
		return fmt.Sprintf("user:%s", user.ID)
	}
	return KeyByIP(r)
}

// KeyByUserAndPath generates rate limit key based on user ID and path
func KeyByUserAndPath(r *http.Request) string {
	user := GetUser(r.Context())
	userKey := GetRealIP(r)
	if user != nil && user.ID != "" {
		userKey = user.ID
	}
	return fmt.Sprintf("user:%s:path:%s", userKey, r.URL.Path)
}

// KeyByAnonIP generates a rate limit key only for anonymous users, keyed by IP.
// Returns empty string for authenticated users (skipping the limit).
func KeyByAnonIP(r *http.Request) string {
	user := GetUser(r.Context())
	if user != nil && !user.IsAnonymous() {
		return "" // Authenticated users are not rate limited by this config
	}
	ip := GetRealIP(r)
	return fmt.Sprintf("anon-ip:%s", ip)
}

// Common rate limit configurations

// GlobalRateLimit applies to all requests from an IP
var GlobalRateLimit = RateLimitConfig{
	Requests: 100,
	Window:   1 * time.Minute,
	KeyFunc:  KeyByIP,
}

// AuthRateLimit applies to authentication endpoints
var AuthRateLimit = RateLimitConfig{
	Requests: 5,
	Window:   5 * time.Minute,
	KeyFunc:  KeyByIP,
}

// FileUploadRateLimit applies to file upload endpoints (per user)
var FileUploadRateLimit = RateLimitConfig{
	Requests: 10,
	Window:   1 * time.Hour,
	KeyFunc:  KeyByUser,
}

// JobCreationRateLimit applies to job creation (per user)
var JobCreationRateLimit = RateLimitConfig{
	Requests: 20,
	Window:   1 * time.Minute,
	KeyFunc:  KeyByUser,
}

// WebhookRateLimit applies to webhook endpoints
var WebhookRateLimit = RateLimitConfig{
	Requests: 100,
	Window:   1 * time.Minute,
	KeyFunc:  KeyByIP,
}

// AnonFileUploadRateLimit is a stricter IP-based limit for anonymous file uploads.
// Since anonymous users can clear cookies, this IP-based limit acts as a hard backstop.
// Authenticated users are not affected (KeyByAnonIP returns "" for them, skipping the limit).
var AnonFileUploadRateLimit = RateLimitConfig{
	Requests: 5,
	Window:   1 * time.Hour,
	KeyFunc:  KeyByAnonIP,
}

// AnonJobCreationRateLimit is a stricter IP-based limit for anonymous job creation.
// Authenticated users are not affected.
var AnonJobCreationRateLimit = RateLimitConfig{
	Requests: 10,
	Window:   1 * time.Hour,
	KeyFunc:  KeyByAnonIP,
}
