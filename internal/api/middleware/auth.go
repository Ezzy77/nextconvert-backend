package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/jwt"
	"github.com/clerk/clerk-sdk-go/v2/user"
	"github.com/google/uuid"
)

type contextKey string

const (
	UserContextKey contextKey = "user"

	// AnonCookieName is the cookie used to track anonymous users per-device
	AnonCookieName = "__nc_anon"

	// AnonIDPrefix is prepended to anonymous user IDs
	AnonIDPrefix = "anon:"

	// AnonCookieMaxAge is the max-age of the anonymous cookie (30 days)
	AnonCookieMaxAge = 30 * 24 * 60 * 60
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

// IsAnonymous returns true if the user is an anonymous (not logged in) user
func (u *User) IsAnonymous() bool {
	if u == nil {
		return true
	}
	return strings.HasPrefix(u.ID, AnonIDPrefix)
}

// TierLookup resolves user tier from storage (e.g. user_profiles)
type TierLookup interface {
	GetTier(ctx context.Context, userID string) string
}

// ClerkAuthMiddleware handles Clerk authentication
type ClerkAuthMiddleware struct {
	secretKey  string
	tierLookup TierLookup
	secure     bool // true in production (HTTPS)
}

// NewClerkAuthMiddleware creates a new Clerk auth middleware instance
func NewClerkAuthMiddleware(secretKey string, tierLookup TierLookup) *ClerkAuthMiddleware {
	// Initialize Clerk SDK
	clerk.SetKey(secretKey)
	return &ClerkAuthMiddleware{
		secretKey:  secretKey,
		tierLookup: tierLookup,
		secure:     false,
	}
}

// NewClerkAuthMiddlewareWithOptions creates a new Clerk auth middleware with options
func NewClerkAuthMiddlewareWithOptions(secretKey string, tierLookup TierLookup, secure bool) *ClerkAuthMiddleware {
	clerk.SetKey(secretKey)
	return &ClerkAuthMiddleware{
		secretKey:  secretKey,
		tierLookup: tierLookup,
		secure:     secure,
	}
}

// getOrCreateAnonID reads the anonymous cookie or generates a new one.
// Returns the anon user ID (e.g. "anon:550e8400-...") and sets the cookie if new.
func (m *ClerkAuthMiddleware) getOrCreateAnonID(w http.ResponseWriter, r *http.Request) string {
	// Check for existing cookie
	if cookie, err := r.Cookie(AnonCookieName); err == nil && cookie.Value != "" {
		// Validate it looks like a UUID (basic check)
		if len(cookie.Value) >= 32 {
			return fmt.Sprintf("%s%s", AnonIDPrefix, cookie.Value)
		}
	}

	// Generate a new anonymous ID
	anonUUID := uuid.New().String()

	// Set cookie
	sameSite := http.SameSiteLaxMode
	if m.secure {
		sameSite = http.SameSiteNoneMode
	}
	http.SetCookie(w, &http.Cookie{
		Name:     AnonCookieName,
		Value:    anonUUID,
		Path:     "/",
		MaxAge:   AnonCookieMaxAge,
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: sameSite,
	})

	return fmt.Sprintf("%s%s", AnonIDPrefix, anonUUID)
}

// Handler returns the HTTP middleware handler for Clerk authentication
func (m *ClerkAuthMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			// Generate per-device anonymous identity via cookie
			anonID := m.getOrCreateAnonID(w, r)
			ctx := context.WithValue(r.Context(), UserContextKey, &User{
				ID:   anonID,
				Tier: "free",
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

		// Resolve tier from user_profiles
		tier := "free"
		if m.tierLookup != nil {
			if t := m.tierLookup.GetTier(r.Context(), userID); t != "" {
				tier = t
			}
		}

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

// RequireAuth returns middleware that requires authentication (rejects anonymous users)
func (m *ClerkAuthMiddleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r.Context())
		if user == nil || user.IsAnonymous() {
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
