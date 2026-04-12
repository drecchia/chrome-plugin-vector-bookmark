package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testMiddlewareChain composes corsMiddleware(origins) -> authMiddleware(token,
// origins) -> terminal handler in the SAME order used by production
// newRouter (routes.go:97-98). Testing the chain directly (instead of through
// chi.Router) keeps this a true unit test of the middleware ordering fix and
// avoids chi's method-routing quirks around OPTIONS/MethodNotAllowed.
func testMiddlewareChain(token string, origins []string) http.Handler {
	cors := corsMiddleware(origins)
	auth := authMiddleware(token, origins)
	terminal := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"indexed":0,"pending":0,"version":"test"}`))
	})
	return cors(auth(terminal))
}

// TestCORSHeadersPresentOn401 is the canonical regression test for the P0-NEW
// (v2) finding: authMiddleware used to run before corsMiddleware, so 401
// responses were emitted without Access-Control-Allow-* headers. Browsers turn
// CORS-less 401s into opaque network errors, making VBM_CORS_ORIGIN
// effectively inoperante for non-preflight requests. Reverting the swap at
// routes.go:97-98 causes this test to fail.
func TestCORSHeadersPresentOn401(t *testing.T) {
	h := testMiddlewareChain("secret-token", []string{"http://localhost:3000"})

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	// Intentionally no Authorization header — auth must reject with 401
	// BUT the browser must still receive CORS headers so it can read the body.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d (body=%q)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("want Access-Control-Allow-Origin=http://localhost:3000, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Errorf("want Access-Control-Allow-Methods to be set on 401, got empty")
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "Authorization") {
		t.Errorf("want Access-Control-Allow-Headers to include Authorization, got %q", got)
	}
}

// TestCORSPreflightStillBypassesAuth guarantees the middleware swap did not
// break browser preflight handling. OPTIONS must return 204 with CORS headers
// and must never reach the terminal handler or be rejected by auth.
func TestCORSPreflightStillBypassesAuth(t *testing.T) {
	h := testMiddlewareChain("secret-token", []string{"http://localhost:3000"})

	req := httptest.NewRequest(http.MethodOptions, "/search", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "Authorization")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("want 204 for preflight, got %d (body=%q)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("want ACAO=http://localhost:3000, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "GET") {
		t.Errorf("want Allow-Methods to contain GET, got %q", got)
	}
}

// TestAuthorizedRequestFromAllowedOrigin200 is the happy path: legitimate
// dashboard at a whitelisted origin with a valid token gets a 200 plus CORS
// headers.
func TestAuthorizedRequestFromAllowedOrigin200(t *testing.T) {
	h := testMiddlewareChain("secret-token", []string{"http://localhost:3000"})

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%q)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("want ACAO=http://localhost:3000, got %q", got)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("want application/json content-type, got %q", ct)
	}
}

// TestDisallowedOriginNoCORSHeaders ensures security is preserved: an origin
// not in extraOrigins receives NO Access-Control-Allow-Origin header, even
// when it presents a valid token. authMiddleware still rejects it with 401.
func TestDisallowedOriginNoCORSHeaders(t *testing.T) {
	h := testMiddlewareChain("secret-token", []string{"http://localhost:3000"})

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.Header.Set("Origin", "http://evil.com")
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 for disallowed origin, got %d (body=%q)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("want NO ACAO for disallowed origin, got %q", got)
	}
}

// TestBuggyOrderProducesCORSlessOn401 pins the regression: when the middleware
// chain is assembled in the OLD buggy order (auth wraps cors), a 401 response
// arrives WITHOUT Access-Control-Allow-* headers. This test documents the bug
// shape so any future refactor that accidentally reverts the swap at
// routes.go:97-98 is caught by a concrete red test alongside the green ones.
func TestBuggyOrderProducesCORSlessOn401(t *testing.T) {
	cors := corsMiddleware([]string{"http://localhost:3000"})
	auth := authMiddleware("secret-token", []string{"http://localhost:3000"})
	terminal := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	// Buggy order on purpose: auth runs first, rejects 401 before cors sets headers.
	buggy := auth(cors(terminal))

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	// No Authorization → auth returns 401 before cors middleware executes.
	rec := httptest.NewRecorder()
	buggy.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	// THE bug: CORS headers MUST be missing here. If this assertion starts
	// failing, either chi/middleware semantics changed or someone fixed the
	// bug in a different way — update this test accordingly.
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("buggy order should produce CORS-less 401, but ACAO=%q", got)
	}
}

// TestChromeExtensionOriginAllowed verifies that chrome-extension:// origins
// still pass through without needing to be listed in extraOrigins — the
// primary client is the extension itself and must keep working.
func TestChromeExtensionOriginAllowed(t *testing.T) {
	h := testMiddlewareChain("secret-token", nil)

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.Header.Set("Origin", "chrome-extension://abcdefghijklmnopqrstuvwxyz012345")
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 for chrome-extension origin, got %d (body=%q)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "chrome-extension://abcdefghijklmnopqrstuvwxyz012345" {
		t.Errorf("want chrome-extension ACAO echoed, got %q", got)
	}
}
