package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tushar/dis-job-queue/api/internal/config"
	"github.com/tushar/dis-job-queue/api/internal/middleware"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	db  *pgxpool.Pool
	cfg *config.Config
}

func NewAuthHandler(db *pgxpool.Pool, cfg *config.Config) *AuthHandler {
	return &AuthHandler{db: db, cfg: cfg}
}

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

// Register godoc
// @Summary      Register a new user
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body body registerRequest true "Registration payload"
// @Success      201 {object} map[string]string
// @Router       /auth/register [post]
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Email == "" || req.Password == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "email, password, and name are required")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var userID string
	err = h.db.QueryRow(r.Context(),
		`INSERT INTO users (email, password_hash, name) VALUES ($1,$2,$3) RETURNING id`,
		req.Email, string(hash), req.Name,
	).Scan(&userID)
	if err != nil {
		writeError(w, http.StatusConflict, "email already registered")
		return
	}

	token, refresh, err := h.generateTokens(userID, req.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token generation failed")
		return
	}
	if err := h.storeRefreshToken(r.Context(), userID, refresh, h.cfg.RefreshExpiry); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"access_token":  token,
		"refresh_token": refresh,
		"user_id":       userID,
	})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Login godoc
// @Summary      Login
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body body loginRequest true "Credentials"
// @Success      200 {object} map[string]string
// @Router       /auth/login [post]
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	var userID, hash, name string
	err := h.db.QueryRow(r.Context(),
		`SELECT id, password_hash, name FROM users WHERE email=$1`, req.Email,
	).Scan(&userID, &hash, &name)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, refresh, err := h.generateTokens(userID, req.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token generation failed")
		return
	}
	if err := h.storeRefreshToken(r.Context(), userID, refresh, h.cfg.RefreshExpiry); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"access_token":  token,
		"refresh_token": refresh,
		"user_id":       userID,
		"name":          name,
	})
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "refresh_token required")
		return
	}

	th := sha256hex(req.RefreshToken)
	var userID, email string
	var expiresAt time.Time
	err := h.db.QueryRow(r.Context(),
		`SELECT u.id, u.email, rt.expires_at
		 FROM refresh_tokens rt JOIN users u ON u.id=rt.user_id
		 WHERE rt.token_hash=$1 AND rt.revoked_at IS NULL`,
		th,
	).Scan(&userID, &email, &expiresAt)
	if err != nil || time.Now().After(expiresAt) {
		writeError(w, http.StatusUnauthorized, "invalid or expired refresh token")
		return
	}

	// Rotate
	h.db.Exec(r.Context(), `UPDATE refresh_tokens SET revoked_at=now() WHERE token_hash=$1`, th)

	token, newRefresh, err := h.generateTokens(userID, email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token generation failed")
		return
	}
	if err := h.storeRefreshToken(r.Context(), userID, newRefresh, h.cfg.RefreshExpiry); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"access_token":  token,
		"refresh_token": newRefresh,
	})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.RefreshToken != "" {
		h.db.Exec(r.Context(),
			`UPDATE refresh_tokens SET revoked_at=now() WHERE token_hash=$1`,
			sha256hex(req.RefreshToken))
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	var id, email, name string
	var createdAt time.Time
	if err := h.db.QueryRow(r.Context(),
		`SELECT id, email, name, created_at FROM users WHERE id=$1`, userID,
	).Scan(&id, &email, &name, &createdAt); err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":         id,
		"email":      email,
		"name":       name,
		"created_at": createdAt,
	})
}

func (h *AuthHandler) generateTokens(userID, email string) (string, string, error) {
	claims := middleware.Claims{
		UserID: userID,
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(h.cfg.JWTExpiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        uuid.New().String(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(h.cfg.JWTSecret))
	if err != nil {
		return "", "", err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	return signed, hex.EncodeToString(buf), nil
}

func (h *AuthHandler) storeRefreshToken(ctx context.Context, userID, token string, ttl time.Duration) error {
	_, err := h.db.Exec(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1,$2,$3)`,
		userID, sha256hex(token), time.Now().Add(ttl),
	)
	return err
}

func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
