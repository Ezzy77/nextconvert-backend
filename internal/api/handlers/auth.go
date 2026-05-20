package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/nextconvert/backend/internal/api/middleware"
	"github.com/nextconvert/backend/internal/shared/database"
	"go.uber.org/zap"
)

// AuthHandler handles authentication-adjacent operations like anonymous-to-user claim.
type AuthHandler struct {
	db     *database.Postgres
	logger *zap.Logger
	secure bool
}

// NewAuthHandler creates a new auth handler.
func NewAuthHandler(db *database.Postgres, logger *zap.Logger, secure bool) *AuthHandler {
	return &AuthHandler{db: db, logger: logger, secure: secure}
}

// ClaimAnonResponse is the response body for ClaimAnon.
type ClaimAnonResponse struct {
	FilesClaimed int64 `json:"filesClaimed"`
	JobsClaimed  int64 `json:"jobsClaimed"`
}

// ClaimAnon transfers ownership of files/jobs from the caller's anon cookie ID
// to their signed-in Clerk user ID, then clears the anon cookie.
// Requires a signed-in user; no-op if no anon cookie is present.
func (h *AuthHandler) ClaimAnon(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r.Context())
	if user == nil || user.IsAnonymous() {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}

	cookie, err := r.Cookie(middleware.AnonCookieName)
	if err != nil || cookie.Value == "" || len(cookie.Value) < 32 {
		// No anon identity to claim — return zero counts.
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ClaimAnonResponse{})
		return
	}

	anonID := middleware.AnonIDPrefix + cookie.Value

	// Guard against a malformed cookie ever colliding with a signed-in ID.
	if !strings.HasPrefix(anonID, middleware.AnonIDPrefix) || anonID == user.ID {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ClaimAnonResponse{})
		return
	}

	var resp ClaimAnonResponse

	fileRes, err := h.db.Pool.Exec(r.Context(),
		`UPDATE files SET user_id = $1 WHERE user_id = $2`,
		user.ID, anonID,
	)
	if err != nil {
		h.logger.Error("Failed to claim anonymous files", zap.Error(err), zap.String("user_id", user.ID))
		http.Error(w, "failed to claim files", http.StatusInternalServerError)
		return
	}
	resp.FilesClaimed = fileRes.RowsAffected()

	jobRes, err := h.db.Pool.Exec(r.Context(),
		`UPDATE jobs SET user_id = $1 WHERE user_id = $2`,
		user.ID, anonID,
	)
	if err != nil {
		h.logger.Error("Failed to claim anonymous jobs", zap.Error(err), zap.String("user_id", user.ID))
		http.Error(w, "failed to claim jobs", http.StatusInternalServerError)
		return
	}
	resp.JobsClaimed = jobRes.RowsAffected()

	// Clear the anon cookie so the same anon ID can't be re-claimed by a different account.
	sameSite := http.SameSiteLaxMode
	if h.secure {
		sameSite = http.SameSiteNoneMode
	}
	http.SetCookie(w, &http.Cookie{
		Name:     middleware.AnonCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: sameSite,
	})

	h.logger.Info("Anonymous identity claimed",
		zap.String("user_id", user.ID),
		zap.Int64("files_claimed", resp.FilesClaimed),
		zap.Int64("jobs_claimed", resp.JobsClaimed),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
