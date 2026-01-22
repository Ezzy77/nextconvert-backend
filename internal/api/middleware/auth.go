package middleware

import (
	"context"
	"net/http"
	"strings"
)

type contextKey string

const (
	UserContextKey contextKey = "user"
)

// User represents an authenticated user
type User struct {
	ID    string
	Email string
	Tier  string
}

// Auth returns authentication middleware
func Auth(jwtSecret string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				// For now, allow anonymous access with default user
				ctx := context.WithValue(r.Context(), UserContextKey, &User{
					ID:    "anonymous",
					Email: "anonymous@local",
					Tier:  "free",
				})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Extract Bearer token
			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" {
				http.Error(w, "Invalid authorization header", http.StatusUnauthorized)
				return
			}

			token := parts[1]

			// TODO: Implement JWT validation
			// For now, just pass through
			_ = token

			ctx := context.WithValue(r.Context(), UserContextKey, &User{
				ID:    "user-123",
				Email: "user@example.com",
				Tier:  "free",
			})

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUser retrieves the user from context
func GetUser(ctx context.Context) *User {
	user, ok := ctx.Value(UserContextKey).(*User)
	if !ok {
		return nil
	}
	return user
}
