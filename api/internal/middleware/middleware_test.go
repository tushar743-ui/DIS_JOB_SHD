package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-secret"

func signedToken(t *testing.T, secret string, userID string, expiry time.Duration, method jwt.SigningMethod) string {
	t.Helper()
	claims := &Claims{
		UserID: userID,
		Email:  "user@example.test",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiry)),
		},
	}
	tok := jwt.NewWithClaims(method, claims)
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("signing token: %v", err)
	}
	return s
}

func okHandler(reached *bool, gotUserID *string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reached = true
		*gotUserID = UserIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
}

func TestAuthAcceptsValidTokenAndPopulatesContext(t *testing.T) {
	var reached bool
	var gotUserID string
	h := Auth(testSecret)(okHandler(&reached, &gotUserID))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+signedToken(t, testSecret, "user-123", time.Minute, jwt.SigningMethodHS256))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if !reached {
		t.Fatal("handler was not reached")
	}
	if gotUserID != "user-123" {
		t.Errorf("UserIDFromContext = %q, want %q", gotUserID, "user-123")
	}
}

func TestAuthRejects(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
		wantBody   string
	}{
		{
			name:       "no header at all",
			authHeader: "",
			wantBody:   `{"error":"missing token"}`,
		},
		{
			name:       "token without Bearer scheme",
			authHeader: signedToken(t, testSecret, "user-123", time.Minute, jwt.SigningMethodHS256),
			wantBody:   `{"error":"missing token"}`,
		},
		{
			name:       "malformed token",
			authHeader: "Bearer not.a.jwt",
			wantBody:   `{"error":"invalid token"}`,
		},
		{
			name:       "expired token",
			authHeader: "Bearer " + signedToken(t, testSecret, "user-123", -time.Minute, jwt.SigningMethodHS256),
			wantBody:   `{"error":"invalid token"}`,
		},
		{
			name:       "token signed with a different secret",
			authHeader: "Bearer " + signedToken(t, "wrong-secret", "user-123", time.Minute, jwt.SigningMethodHS256),
			wantBody:   `{"error":"invalid token"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var reached bool
			var gotUserID string
			h := Auth(testSecret)(okHandler(&reached, &gotUserID))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
			if got := rec.Body.String(); got != tc.wantBody+"\n" {
				t.Errorf("body = %q, want %q", got, tc.wantBody+"\n")
			}
			if reached {
				t.Error("handler ran despite failed auth")
			}
		})
	}
}

func TestAuthRejectsUnsignedToken(t *testing.T) {
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, &Claims{
		UserID: "attacker",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	unsigned, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("building none-alg token: %v", err)
	}

	var reached bool
	var gotUserID string
	h := Auth(testSecret)(okHandler(&reached, &gotUserID))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+unsigned)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for alg=none", rec.Code)
	}
	if reached {
		t.Error("handler ran for an unsigned token")
	}
}

func TestUserIDFromContextIsEmptyWhenUnset(t *testing.T) {
	if got := UserIDFromContext(httptest.NewRequest(http.MethodGet, "/", nil).Context()); got != "" {
		t.Errorf("UserIDFromContext on a bare context = %q, want empty", got)
	}
}

func TestCORSShortCircuitsPreflight(t *testing.T) {
	var reached bool
	h := CORS([]string{"*"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/queues", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight status = %d, want 204", rec.Code)
	}
	if reached {
		t.Error("preflight was passed through to the handler")
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("preflight response is missing Access-Control-Allow-Headers")
	}
}

func TestCORSPassesThroughRealRequests(t *testing.T) {
	var reached bool
	h := CORS([]string{"*"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/queues", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !reached {
		t.Error("GET was not passed through")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "*")
	}
}

func TestAuthAcceptsQueryTokenOnlyForWebSocketUpgrades(t *testing.T) {
	const secret = "test-secret"
	token := signedToken(t, secret, "user-1", time.Hour, jwt.SigningMethodHS256)

	tests := []struct {
		name     string
		upgrade  bool
		wantCode int
	}{
		{"websocket upgrade may authenticate via query", true, http.StatusOK},
		{"plain request may not authenticate via query", false, http.StatusUnauthorized},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := Auth(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := UserIDFromContext(r.Context()); got != "user-1" {
					t.Errorf("user id in context = %q, want user-1", got)
				}
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/p1/events?token="+token, nil)
			if tc.upgrade {
				req.Header.Set("Upgrade", "websocket")
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantCode)
			}
		})
	}
}

func TestAuthRejectsTokenSignedWithAnotherSecret(t *testing.T) {
	token := signedToken(t, "attacker-secret", "user-1", time.Hour, jwt.SigningMethodHS256)

	h := Auth("real-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("a token signed with the wrong secret reached the handler")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orgs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestAuthRejectsExpiredToken(t *testing.T) {
	const secret = "test-secret"
	token := signedToken(t, secret, "user-1", -time.Minute, jwt.SigningMethodHS256)

	h := Auth(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("an expired token reached the handler")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orgs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestCORSHonoursAnOriginAllowlist(t *testing.T) {
	allowed := "https://dashboard.example.com"
	h := CORS([]string{allowed})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		origin string
		want   string
	}{
		{allowed, allowed},
		{"https://evil.example.com", ""},
	}

	for _, tc := range tests {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/queues", nil)
		req.Header.Set("Origin", tc.origin)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != tc.want {
			t.Errorf("origin %s: Access-Control-Allow-Origin = %q, want %q", tc.origin, got, tc.want)
		}
	}
}

func TestRedactedPathHidesWebSocketToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/p1/events?token=super-secret&x=1", nil)

	got := redactedPath(req)
	if strings.Contains(got, "super-secret") {
		t.Fatalf("logged path %q leaks the access token", got)
	}
	if !strings.Contains(got, "token=redacted") {
		t.Fatalf("logged path %q does not mark the token as redacted", got)
	}
}
