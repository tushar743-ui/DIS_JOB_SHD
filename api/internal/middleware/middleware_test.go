package middleware

import (
	"net/http"
	"net/http/httptest"
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

// okHandler records whether the request made it past the middleware, and what
// user id the middleware put on the context.
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
			// Without the scheme check a raw token would fall through to the parser.
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
			// This is the case the web client must recognise and refresh on.
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

// A token whose header says "alg":"none" must not be honoured, or anyone could
// mint credentials.
//
// Note this asserts the behaviour, not the implementation: jwt/v5 already
// refuses none-alg tokens unless the parser opts in, so this still passes if
// the SigningMethodHMAC guard in Auth is deleted. Keep the guard anyway — it is
// the thing that stops an algorithm swap if that library default ever changes.
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
	h := CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	h := CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
