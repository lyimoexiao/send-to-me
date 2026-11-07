package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"sendtome/internal/config"
	"sendtome/internal/settings"
	"sendtome/internal/storage"
	"sendtome/internal/store"
)

const baseContentSecurityPolicy = "default-src 'self'; img-src 'self' data: https://bing.biturl.top https://*.bing.com https://picsum.photos https://fastly.picsum.photos%s; style-src 'self' 'unsafe-inline'; " +
	"script-src 'self'; font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; " +
	"frame-ancestors 'none'; object-src 'none'"

type Server struct {
	cfg     config.Config
	store   *store.Store
	storage storage.Storage
	logger  *slog.Logger
	assets  fs.FS
	handler http.Handler

	adminPassword  string
	adminKey       []byte
	backgroundsDir string
	loginGuard     *loginGuard

	revokedMu sync.Mutex
	revoked   map[string]time.Time

	settingsMu sync.RWMutex
	runtime    settings.Settings
}

func New(cfg config.Config, st *store.Store, payloads storage.Storage, logger *slog.Logger, assets fs.FS) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		cfg:            cfg,
		store:          st,
		storage:        payloads,
		logger:         logger,
		assets:         assets,
		adminPassword:  cfg.AdminPassword,
		backgroundsDir: filepath.Join(cfg.DataDir, "backgrounds"),
		loginGuard:     newLoginGuard(),
		revoked:        make(map[string]time.Time),
		adminKey:       make([]byte, 32),
	}
	if _, err := rand.Read(s.adminKey); err != nil {
		logger.Error("generate admin session key failed", "error", err)
	}
	if err := os.MkdirAll(s.backgroundsDir, 0o750); err != nil {
		logger.Error("create backgrounds directory failed", "error", err)
	}
	s.runtime = settings.Defaults()
	s.runtime.MaxUploadBytes = cfg.MaxUploadBytes
	s.runtime.MaxFiles = cfg.MaxFiles
	s.runtime.MaxTextBytes = cfg.MaxTextBytes
	s.runtime.DefaultExpirySeconds = int64(cfg.DefaultExpiry.Seconds())
	s.runtime.MaxExpirySeconds = int64(cfg.MaxExpiry.Seconds())
	s.loadStoredSettings()
	s.handler = s.buildHandler()
	return s
}

// contentSecurityPolicy allows image hosts added through the admin page.
func (s *Server) contentSecurityPolicy() string {
	hosts := make(map[string]bool)
	for _, bg := range s.settingsSnapshot().Backgrounds {
		if bg.Kind != "url" {
			continue
		}
		parsed, err := url.Parse(bg.URL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			continue
		}
		host := parsed.Scheme + "://" + parsed.Host
		if strings.Contains(baseContentSecurityPolicy, host) {
			continue
		}
		hosts[host] = true
	}
	if len(hosts) == 0 {
		return fmt.Sprintf(baseContentSecurityPolicy, "")
	}
	sorted := make([]string, 0, len(hosts))
	for host := range hosts {
		sorted = append(sorted, host)
	}
	sort.Strings(sorted)
	return fmt.Sprintf(baseContentSecurityPolicy, " "+strings.Join(sorted, " "))
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) buildHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("POST /api/shares", s.handleCreateShare)
	mux.HandleFunc("GET /api/shares/{code}", s.handleMetadata)
	mux.HandleFunc("GET /api/shares/{code}/download", s.handleDownload)
	mux.HandleFunc("GET /api/shares/{code}/text", s.handleRevealText)
	mux.HandleFunc("GET /media/backgrounds/{name}", s.handleBackgroundMedia)
	mux.HandleFunc("GET /api/admin/session", s.handleAdminSession)
	mux.HandleFunc("POST /api/admin/login", s.handleAdminLogin)
	mux.HandleFunc("POST /api/admin/logout", s.handleAdminLogout)
	mux.Handle("GET /api/admin/settings", s.requireAdmin(http.HandlerFunc(s.handleAdminGetSettings)))
	mux.Handle("PUT /api/admin/settings", s.requireAdmin(http.HandlerFunc(s.handleAdminPutSettings)))
	mux.Handle("POST /api/admin/backgrounds", s.requireAdmin(http.HandlerFunc(s.handleAdminAddBackground)))
	mux.Handle("POST /api/admin/backgrounds/upload", s.requireAdmin(http.HandlerFunc(s.handleAdminUploadBackground)))
	mux.Handle("DELETE /api/admin/backgrounds/{id}", s.requireAdmin(http.HandlerFunc(s.handleAdminDeleteBackground)))
	mux.Handle("/", s.staticHandler())

	return s.recoverPanics(s.logRequests(s.securityHeaders(mux)))
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		s.logger.Info("request",
			"method", r.Method,
			"path", redactedPath(r.URL.Path),
			"status", recorder.status,
			"bytes", recorder.bytes,
			"duration_ms", time.Since(started).Milliseconds(),
			"ip", clientIP(r, s.cfg.TrustProxy),
		)
	})
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("panic while serving request",
					"panic", recovered,
					"path", redactedPath(r.URL.Path),
					"stack", string(debug.Stack()),
				)
				writeAPIError(w, http.StatusInternalServerError, "server_error", "服务器出了点问题，请稍后再试")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) staticHandler() http.Handler {
	if s.assets == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
	}
	fileServer := http.FileServer(http.FS(s.assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeAPIError(w, http.StatusNotFound, "not_found", "接口不存在")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		cleaned := path.Clean("/" + r.URL.Path)
		name := strings.TrimPrefix(cleaned, "/")
		if name != "" {
			if info, err := fs.Stat(s.assets, name); err == nil && !info.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "public, max-age=3600")
				}
				w.Header().Set("Content-Security-Policy", s.contentSecurityPolicy())
				fileServer.ServeHTTP(w, r)
				return
			}
			if strings.HasPrefix(name, "assets/") {
				http.NotFound(w, r)
				return
			}
		}
		s.serveIndex(w, r)
	})
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	body, err := fs.ReadFile(s.assets, "index.html")
	if err != nil {
		http.Error(w, "frontend is not built", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Security-Policy", s.contentSecurityPolicy())
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		s.logger.Error("database health check failed", "error", err)
		writeAPIError(w, http.StatusServiceUnavailable, "database_unavailable", "数据库暂时不可用")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	current := s.settingsSnapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"maxUploadBytes":       current.MaxUploadBytes,
		"maxFiles":             current.MaxFiles,
		"maxTextBytes":         current.MaxTextBytes,
		"defaultExpirySeconds": current.DefaultExpirySeconds,
		"maxExpirySeconds":     current.MaxExpirySeconds,
		"defaultMaxDownloads":  current.DefaultMaxDownloads,
		"backgrounds":          current.Backgrounds,
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(buf []byte) (int, error) {
	n, err := r.ResponseWriter.Write(buf)
	r.bytes += int64(n)
	return n, err
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func redactedPath(requestPath string) string {
	parts := strings.Split(requestPath, "/")
	// /api/shares/{code}/...
	if len(parts) >= 4 && parts[1] == "api" && parts[2] == "shares" && parts[3] != "" {
		parts[3] = "{code}"
	}
	// /p/{code}
	if len(parts) >= 3 && parts[1] == "p" && parts[2] != "" {
		parts[2] = "{code}"
	}
	return strings.Join(parts, "/")
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorEnvelope{Error: errorBody{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
