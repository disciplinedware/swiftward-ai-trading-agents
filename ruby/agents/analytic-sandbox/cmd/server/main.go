package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"analytic-sandbox/internal/mcp_handlers"
	"analytic-sandbox/internal/sandbox"
	"analytic-sandbox/internal/session"

	"github.com/joho/godotenv"
	"github.com/mark3labs/mcp-go/server"
)

var (
	addr              = flag.String("addr", ":9091", "Streamable HTTP address to listen on")
	inactivityTimeout = flag.Duration("inactivity-timeout", 30*time.Minute, "Stop containers after this period of inactivity")
)

const (
	defaultMemoryMB       = 512
	maxMemoryMB           = 8192
	defaultCpuCount       = 1
	maxCpuCount           = 16
	defaultDiskMB         = 200
	maxDiskMB             = 51200
	defaultTimeoutSeconds = 120
	maxTimeoutSeconds     = 600
)

type SandboxOptions struct {
	AllowNetwork   bool
	WorkdirUUID    string
	MemoryMB       int
	CpuCount       int
	DiskMB         int
	TimeoutSeconds int
}

func parseSandboxHeaders(r *http.Request) SandboxOptions {
	opts := SandboxOptions{
		AllowNetwork:   false,
		MemoryMB:       defaultMemoryMB,
		CpuCount:       defaultCpuCount,
		DiskMB:         defaultDiskMB,
		TimeoutSeconds: defaultTimeoutSeconds,
	}

	if v := r.Header.Get("X-Sandbox-Allow-Network"); v != "" {
		opts.AllowNetwork = strings.EqualFold(v, "true") || v == "1"
	}

	if v := r.Header.Get("X-Sandbox-Workdir-UUID"); v != "" {
		opts.WorkdirUUID = v
	}

	if v := r.Header.Get("X-Sandbox-Memory-MB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			opts.MemoryMB = n
		}
	}
	if v := r.Header.Get("X-Sandbox-Cpu-Count"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			opts.CpuCount = n
		}
	}
	if v := r.Header.Get("X-Sandbox-Disk-MB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			opts.DiskMB = n
		}
	}
	if v := r.Header.Get("X-Sandbox-Timeout-Seconds"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			opts.TimeoutSeconds = n
		}
	}

	if opts.MemoryMB < 1 {
		opts.MemoryMB = defaultMemoryMB
	}
	if opts.MemoryMB > maxMemoryMB {
		opts.MemoryMB = maxMemoryMB
	}
	if opts.CpuCount < 1 {
		opts.CpuCount = defaultCpuCount
	}
	if opts.CpuCount > maxCpuCount {
		opts.CpuCount = maxCpuCount
	}
	if opts.DiskMB < 1 {
		opts.DiskMB = defaultDiskMB
	}
	if opts.DiskMB > maxDiskMB {
		opts.DiskMB = maxDiskMB
	}
	if opts.TimeoutSeconds < 1 {
		opts.TimeoutSeconds = defaultTimeoutSeconds
	}
	if opts.TimeoutSeconds > maxTimeoutSeconds {
		opts.TimeoutSeconds = maxTimeoutSeconds
	}

	return opts
}

func main() {
	flag.Parse()

	_ = godotenv.Load()

	authToken := os.Getenv("MCP_AUTH_TOKEN")
	if authToken != "" {
		log.Println("MCP_AUTH_TOKEN is set, authorization enabled")
	} else {
		log.Println("MCP_AUTH_TOKEN not set, running without authorization")
	}

	dm, err := sandbox.NewDockerManager("")
	if err != nil {
		log.Fatalf("Failed to initialize Docker manager: %v", err)
	}

	if err := dm.CleanupOrphans(context.Background()); err != nil {
		log.Printf("Warning: failed to cleanup orphans: %v", err)
	}

	sm := session.NewManager(dm, "data")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sm.StartInactivityMonitor(ctx, *inactivityTimeout)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("Received signal %v, shutting down...", sig)
		sm.Cleanup(context.Background())
		os.Exit(0)
	}()

	dataDir := "data"

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	mux.HandleFunc("/status", statusHandler(sm, dm, authToken))

	handler := NewAppHandler(sm, dm, dataDir, authToken)
	mux.Handle("/", handler)

	log.Printf("Starting stateful MCP server on %s", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func NewAppHandler(sm *session.Manager, dm *sandbox.DockerManager, dataDir, authToken string) http.Handler {
	serverFactory := func(opts SandboxOptions, requestedSessionID string, ctx context.Context) (*session.Session, error) {
		return sm.GetOrCreate(ctx, session.SessionParams{
			AllowNetwork:       opts.AllowNetwork,
			RequestedSessionID: requestedSessionID,
			WorkdirUUID:        opts.WorkdirUUID,
			MemoryMB:           opts.MemoryMB,
			CpuCount:           opts.CpuCount,
			DiskMB:             opts.DiskMB,
			TimeoutSeconds:     opts.TimeoutSeconds,
		}, func(sess *session.Session) (*server.MCPServer, http.Handler) {
			log.Printf("Creating new MCP server instance, allowNetwork: %v, workdirUUID: %s, memoryMB: %d, cpuCount: %d, diskMB: %d, timeoutSeconds: %d",
				opts.AllowNetwork, opts.WorkdirUUID, opts.MemoryMB, opts.CpuCount, opts.DiskMB, opts.TimeoutSeconds)
			s := server.NewMCPServer("mcp-stateful-server", "1.0.0")

			h := mcp_handlers.NewHandlers(s, sess, dm)
			h.Register()

			transportHandler := server.NewStreamableHTTPServer(s, server.WithStateful(true))
			return s, transportHandler
		})
	}

	bridgeHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.Header.Get("Mcp-Session-Id")
		var opts SandboxOptions

		if sessionID == "" && r.Method == http.MethodPost {
			opts = parseSandboxHeaders(r)
		}

		sess, err := serverFactory(opts, sessionID, r.Context())
		if err != nil {
			log.Printf("Error creating/getting session: %v", err)
			if strings.Contains(err.Error(), "session expired") {
				http.Error(w, err.Error(), http.StatusUnauthorized)
			} else {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
			return
		}

		if err := sm.RestartContainerIfNeeded(r.Context(), sess.ID); err != nil {
			log.Printf("Failed to restart container: %v", err)
			http.Error(w, "Failed to restart container", http.StatusInternalServerError)
			return
		}

		sm.UpdateActivity(sess.ID)

		sess.Handler.ServeHTTP(w, r)

		if sessionID == "" {
			newID := w.Header().Get("Mcp-Session-Id")
			if newID != "" {
				sm.Register(newID, sess)
			}
		}
	})

	return authMiddleware(authToken, loggingMiddleware(bridgeHandler))
}

func authMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth == "" {
			http.Error(w, "Unauthorized: Missing Authorization header", http.StatusUnauthorized)
			return
		}

		const bearerPrefix = "Bearer "
		if len(auth) < len(bearerPrefix) || auth[:len(bearerPrefix)] != bearerPrefix {
			http.Error(w, "Unauthorized: Invalid Authorization header format", http.StatusUnauthorized)
			return
		}

		providedToken := auth[len(bearerPrefix):]
		if providedToken != token {
			http.Error(w, "Unauthorized: Invalid token", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[HTTP] Request: %s %s", r.Method, r.URL.String())

		isSSE := r.Header.Get("Accept") == "text/event-stream" || r.URL.Path == "/sse" || r.URL.Query().Has("sessionid")

		if r.Method == http.MethodPost {
			body, err := io.ReadAll(r.Body)
			if err == nil {
				log.Printf("[HTTP] Request Payload: %s", string(body))
				r.Body = io.NopCloser(bytes.NewBuffer(body))
			}
		}

		if isSSE {
			next.ServeHTTP(w, r)
			return
		}

		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)

		log.Printf("[HTTP] Response: %d", rw.statusCode)
		if rw.statusCode != http.StatusOK || (rw.body.Len() > 0 && rw.body.Len() < 10000) {
			bodyStr := rw.body.String()
			if len(bodyStr) > 500 {
				bodyStr = bodyStr[:500] + "..."
			}
			log.Printf("[HTTP] Response Body: %s", bodyStr)
		}
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
	body       bytes.Buffer
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	rw.body.Write(b)
	return rw.ResponseWriter.Write(b)
}

func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func statusHandler(sm *session.Manager, dm *sandbox.DockerManager, authToken string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if authToken != "" {
			auth := r.Header.Get("Authorization")
			const bearerPrefix = "Bearer "
			if auth == "" || len(auth) < len(bearerPrefix) || auth[:len(bearerPrefix)] != bearerPrefix {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			if auth[len(bearerPrefix):] != authToken {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		status := sm.GetStatus()
		json.NewEncoder(w).Encode(status)
	}
}
