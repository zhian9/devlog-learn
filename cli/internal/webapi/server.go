// Package webapi serves the local devlog-learn web UI.
//
// The server is deliberately small: it maps the existing internal packages onto
// JSON endpoints and never reimplements extraction, rendering, or indexing. It
// binds to loopback only, requires a per-process token for every API call, and
// embeds the production frontend bundle so the CLI stays a single binary.
package webapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/pipeline"
)

// Options configures one web server instance.
type Options struct {
	Addr       string
	CacheDir   string
	CursorPath string
	OutDir     string
	ClaudeDir  string
	CodexDir   string
	ClaudeBin  string
	CodexBin   string
	Token      string

	// ClientFor is an optional test/integration hook. When nil the server
	// builds real CLI clients from the provider binaries.
	ClientFor pipeline.ClientFor
}

// Server is an http.Handler. It is safe for concurrent reads; writes are
// serialized so review, question, and export updates cannot interleave.
type Server struct {
	opts   Options
	store  *cache.Store
	token  string
	mux    *http.ServeMux
	static http.Handler
	write  sync.Mutex
	jobs   jobManager

	shutdown     chan struct{}
	shutdownOnce sync.Once
}

// New validates options and builds the local server.
func New(opts Options) (*Server, error) {
	if err := validateAddr(opts.Addr); err != nil {
		return nil, err
	}
	token := strings.TrimSpace(opts.Token)
	if token == "" {
		buf := make([]byte, 24)
		if _, err := rand.Read(buf); err != nil {
			return nil, fmt.Errorf("generate access token: %w", err)
		}
		token = hex.EncodeToString(buf)
	}
	static, err := newStaticHandler()
	if err != nil {
		return nil, fmt.Errorf("load embedded frontend: %w", err)
	}
	s := &Server{
		opts:     opts,
		store:    &cache.Store{Root: opts.CacheDir},
		token:    token,
		mux:      http.NewServeMux(),
		static:   static,
		shutdown: make(chan struct{}),
	}
	s.routes()
	return s, nil
}

// Token is the per-process access token. The caller prints a URL containing it.
func (s *Server) Token() string { return s.token }

// URL returns the browser URL including the token.
func (s *Server) URL(addr net.Addr) string {
	host := addr.String()
	return fmt.Sprintf("http://%s/?token=%s", host, s.token)
}

// Listen binds the configured loopback address. The listener is returned so
// the caller can print the actual port when :0 is used.
func (s *Server) Listen() (net.Listener, error) {
	return net.Listen("tcp", s.opts.Addr)
}

// Serve blocks until the HTTP server stops or ctx is canceled. Cancellation
// stops new work, cancels the active extraction job, and drains HTTP handlers.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	srv := &http.Server{
		Handler:           s,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(l)
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		s.shutdownOnce.Do(func() { close(s.shutdown) })
		s.jobs.cancelAll()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdownErr := srv.Shutdown(shutdownCtx)
		jobErr := s.jobs.wait(shutdownCtx)
		if shutdownErr != nil {
			_ = srv.Close()
		}
		serveErr := <-errCh
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		return errors.Join(shutdownErr, jobErr, serveErr)
	}
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/status", s.handleStatus)
	s.mux.HandleFunc("GET /api/config", s.handleConfig)
	s.mux.HandleFunc("GET /api/projects", s.handleProjects)
	s.mux.HandleFunc("GET /api/sessions", s.handleSessions)
	s.mux.HandleFunc("GET /api/docs", s.handleDocs)
	s.mux.HandleFunc("GET /api/docs/{id}", s.handleDoc)
	s.mux.HandleFunc("GET /api/docs/{id}/markdown", s.handleMarkdown)
	s.mux.HandleFunc("GET /api/docs/{id}/events", s.handleEvents)
	s.mux.HandleFunc("POST /api/docs/{id}/review", s.handleReview)
	s.mux.HandleFunc("GET /api/questions", s.handleQuestions)
	s.mux.HandleFunc("POST /api/questions/{id}/resolve", s.handleResolveQuestion)
	s.mux.HandleFunc("POST /api/questions/{id}/reopen", s.handleReopenQuestion)
	s.mux.HandleFunc("POST /api/export", s.handleExport)
	s.mux.HandleFunc("POST /api/jobs/extract", s.handleStartExtract)
	s.mux.HandleFunc("GET /api/jobs/current", s.handleCurrentJob)
	s.mux.HandleFunc("GET /api/jobs/{id}", s.handleJob)
	s.mux.HandleFunc("GET /api/jobs/{id}/events", s.handleJobEvents)
	s.mux.HandleFunc("POST /api/jobs/{id}/cancel", s.handleCancelJob)
	s.mux.HandleFunc("/", s.handleStatic)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if !isLoopbackHost(r.Host) {
		writeError(w, http.StatusForbidden, "local access only")
		return
	}

	// A token in the URL is exchanged for an HttpOnly cookie and removed from
	// the address bar. The API also accepts the token as a header for the Vite
	// dev server, which proxies JSON but never serves this redirect.
	if supplied := r.URL.Query().Get("token"); supplied != "" && tokenEqual(supplied, s.token) {
		http.SetCookie(w, &http.Cookie{
			Name:     "devlog_token",
			Value:    s.token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   12 * 60 * 60,
		})
		if r.Method == http.MethodGet && r.URL.Path == "/" {
			clean := *r.URL
			query := clean.Query()
			query.Del("token")
			clean.RawQuery = query.Encode()
			http.Redirect(w, r, clean.String(), http.StatusFound)
			return
		}
	}

	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Cache-Control", "no-store")
		if !s.authorized(r) {
			writeError(w, http.StatusUnauthorized, "missing or invalid access token")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(r) {
			writeError(w, http.StatusForbidden, "cross-origin write rejected")
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) authorized(r *http.Request) bool {
	if tokenEqual(r.Header.Get("X-Devlog-Token"), s.token) {
		return true
	}
	if cookie, err := r.Cookie("devlog_token"); err == nil && tokenEqual(cookie.Value, s.token) {
		return true
	}
	return tokenEqual(r.URL.Query().Get("token"), s.token)
}

func (s *Server) acquireCacheWriteLock(w http.ResponseWriter) (*cache.Lock, bool) {
	lock, err := cache.TryLock(s.opts.CacheDir)
	if err == nil {
		return lock, true
	}
	if errors.Is(err, cache.ErrLocked) {
		writeError(w, http.StatusConflict, err.Error()+"; retry after the active writer finishes")
	} else {
		writeError(w, http.StatusInternalServerError, err.Error())
	}
	return nil, false
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.static.ServeHTTP(w, r)
}

func tokenEqual(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func sameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true // non-browser client with a valid token
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return isLoopbackHost(parsed.Host)
}

func isLoopbackHost(hostport string) bool {
	if hostport == "" {
		return false
	}
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validateAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", addr, err)
	}
	if !isLoopbackHost(host) {
		return fmt.Errorf("refusing to bind %q: the web UI only binds to loopback", addr)
	}
	return nil
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; base-uri 'none'; frame-ancestors 'none'; "+
			"img-src 'self' data:; style-src 'self' 'unsafe-inline'; "+
			"script-src 'self'; connect-src 'self'; font-src 'self'")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func decodeJSON(r *http.Request, value any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := decoder.Decode(value); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("empty request body")
		}
		return err
	}
	return nil
}
