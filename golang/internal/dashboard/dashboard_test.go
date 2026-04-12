package dashboard

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"ai-trading-agents/internal/config"
	"ai-trading-agents/internal/platform"
)

func setupDashboard(t *testing.T) chi.Router {
	t.Helper()
	r := chi.NewRouter()
	// Register a health endpoint before dashboard to verify route precedence.
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	svcCtx := platform.NewServiceContext(context.Background(), zap.NewNop(), &config.Config{}, r, []string{"dashboard"})
	svc := NewService(svcCtx)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("dashboard Initialize: %v", err)
	}
	return r
}

func TestDashboardRoutes(t *testing.T) {
	r := setupDashboard(t)

	tests := []struct {
		name         string
		path         string
		wantStatus   int
		wantContains string
		wantType     string
	}{
		{
			name:         "root serves SPA",
			path:         "/",
			wantStatus:   http.StatusOK,
			wantContains: `<div id="root">`,
			wantType:     "text/html; charset=utf-8",
		},
		{
			name:         "SPA route serves index.html fallback",
			path:         "/agents/alpha",
			wantStatus:   http.StatusOK,
			wantContains: `<div id="root">`,
			wantType:     "text/html; charset=utf-8",
		},
		{
			name:         "deep SPA route serves index.html fallback",
			path:         "/market/ETH-USDC/candles",
			wantStatus:   http.StatusOK,
			wantContains: `<div id="root">`,
			wantType:     "text/html; charset=utf-8",
		},
		{
			name:         "existing static file is served directly",
			path:         "/favicon.svg",
			wantStatus:   http.StatusOK,
			wantContains: "<svg",
			wantType:     "image/svg+xml",
		},
		{
			name:       "missing static file falls back to SPA",
			path:       "/assets/nonexistent.js",
			wantStatus: http.StatusOK,
			// File doesn't exist in embedded FS, so SPA fallback serves index.html.
			wantContains: `<div id="root">`,
		},
		{
			name:         "health endpoint takes precedence over catch-all",
			path:         "/health",
			wantStatus:   http.StatusOK,
			wantContains: "ok",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("GET %s: status = %d, want %d", tt.path, rec.Code, tt.wantStatus)
			}
			if tt.wantContains != "" {
				body := rec.Body.String()
				if !strings.Contains(body, tt.wantContains) {
					t.Errorf("GET %s: body does not contain %q\ngot: %s", tt.path, tt.wantContains, body)
				}
			}
			if tt.wantType != "" {
				got := rec.Header().Get("Content-Type")
				if got != tt.wantType {
					t.Errorf("GET %s: Content-Type = %q, want %q", tt.path, got, tt.wantType)
				}
			}
		})
	}
}

func TestFileExists(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html":           {Data: []byte("<html></html>")},
		"assets/app.js":        {Data: []byte("console.log()")},
		"assets/style.css":     {Data: []byte("body{}")},
		"images/logo.webp":     {Data: []byte("webp-data")},
		"manifest.webmanifest": {Data: []byte("{}")},
	}

	tests := []struct {
		path string
		want bool
	}{
		{"/index.html", true},
		{"/assets/app.js", true},
		{"/assets/style.css", true},
		{"/images/logo.webp", true},
		{"/manifest.webmanifest", true},
		{"/nonexistent.js", false},
		{"/assets/missing.css", false},
		{"/", false},
		{"/assets", false}, // directory, not file
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := fileExists(fs.FS(mockFS), tt.path); got != tt.want {
				t.Errorf("fileExists(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
