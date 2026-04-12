package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"go.uber.org/zap"

	"ai-trading-agents/internal/platform"
)

//go:embed all:dist
var distFS embed.FS

// Service serves the embedded dashboard UI as static files on the main chi router.
type Service struct {
	svcCtx *platform.ServiceContext
	log    *zap.Logger
}

func NewService(svcCtx *platform.ServiceContext) *Service {
	return &Service{
		svcCtx: svcCtx,
		log:    svcCtx.Logger().Named("dashboard"),
	}
}

func (s *Service) Initialize() error {
	// Strip the "dist" prefix so files are served from root.
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return err
	}

	// Read index.html once for SPA fallback (avoids http.FileServer's redirect loop
	// where /index.html -> 301 / -> rewrite /index.html -> 301 / ...).
	indexHTML, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		s.log.Warn("Dashboard dist/index.html not found - serving placeholder only. Build the dashboard: cd typescript && npm run build")
		indexHTML = []byte("<html><body><h1>Dashboard not built</h1><p>Run: cd typescript && npm run build</p></body></html>")
	}

	fileServer := http.FileServer(http.FS(sub))

	// Register a catch-all handler for the dashboard SPA.
	// Chi routes are matched in registration order - specific routes (/mcp/*, /health, /v1/*)
	// registered by other services take precedence. This catch-all handles everything else.
	s.svcCtx.Router().Get("/*", func(w http.ResponseWriter, r *http.Request) {
		// If the file exists in the embedded FS, serve it directly.
		// This handles any asset type (.js, .css, .png, .webp, .webmanifest, etc.)
		// without maintaining an extension allowlist.
		if fileExists(sub, r.URL.Path) {
			fileServer.ServeHTTP(w, r)
			return
		}

		// For all other paths, serve index.html directly (SPA routing).
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := w.Write(indexHTML); err != nil {
			s.log.Debug("dashboard write failed (client disconnected?)", zap.Error(err))
		}
	})

	s.log.Info("Dashboard UI registered at /")
	return nil
}

func (s *Service) Start() error {
	<-s.svcCtx.Context().Done()
	return nil
}

func (s *Service) Stop() error {
	s.log.Info("Dashboard stopped")
	return nil
}

// fileExists checks whether path corresponds to a file (not directory) in the FS.
func fileExists(fsys fs.FS, path string) bool {
	// Strip leading slash - fs.FS paths are relative.
	clean := strings.TrimPrefix(path, "/")
	if clean == "" {
		return false
	}
	fi, err := fs.Stat(fsys, clean)
	if err != nil {
		return false
	}
	return !fi.IsDir()
}
