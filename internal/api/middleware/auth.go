package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/jwt"
	"github.com/clerk/clerk-sdk-go/v2/user"
)

type contextKey string

const (
	UserContextKey contextKey = "user"
)

// User represents an authenticated user
type User struct {
	ID        string
	Email     string
	FirstName string
	LastName  string
	ImageURL  string
	Tier      string
}

// ClerkAuthMiddleware handles Clerk authentication
type ClerkAuthMiddleware struct {
	secretKey string
}

// NewClerkAuthMiddleware creates a new Clerk auth middleware instance
func NewClerkAuthMiddleware(secretKey string) *ClerkAuthMiddleware {
	// Initialize Clerk SDK
	clerk.SetKey(secretKey)
	return &ClerkAuthMiddleware{
		secretKey: secretKey,
	}
}

// Handler returns the HTTP middleware handler for Clerk authentication
func (m *ClerkAuthMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			// Allow anonymous access with default user
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

		sessionToken := parts[1]

		// Verify the session token with Clerk
		claims, err := jwt.Verify(r.Context(), &jwt.VerifyParams{
			Token: sessionToken,
		})
		if err != nil {
			http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
			return
		}

		// Get user details from Clerk
		userID := claims.Subject
		clerkUser, err := user.Get(r.Context(), userID)
		if err != nil {
			// If we can't get user details, use basic info from claims
			ctx := context.WithValue(r.Context(), UserContextKey, &User{
				ID:   userID,
				Tier: "free",
			})
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Extract primary email
		var email string
		for _, emailAddr := range clerkUser.EmailAddresses {
			if emailAddr.ID == *clerkUser.PrimaryEmailAddressID {
				email = emailAddr.EmailAddress
				break
			}
		}

		// Default tier is "free"
		// To implement tiers, store them in Clerk's user metadata or your database
		tier := "free"

		// Set user in context
		ctx := context.WithValue(r.Context(), UserContextKey, &User{
			ID:        userID,
			Email:     email,
			FirstName: safeString(clerkUser.FirstName),
			LastName:  safeString(clerkUser.LastName),
			ImageURL:  safeString(clerkUser.ImageURL),
			Tier:      tier,
		})

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAuth returns middleware that requires authentication
func (m *ClerkAuthMiddleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r.Context())
		if user == nil || user.ID == "anonymous" {
			http.Error(w, "Authentication required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// GetUser retrieves the user from context
func GetUser(ctx context.Context) *User {
	user, ok := ctx.Value(UserContextKey).(*User)
	if !ok {
		return nil
	}
	return user
}

// safeString safely dereferences a string pointer
func safeString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
