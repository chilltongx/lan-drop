package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"landrop/internal/storage"
	"rsc.io/qr"
)

const sessionCookie = "lan_drop_session"

//go:embed web/*
var embeddedWeb embed.FS

type Options struct {
	Store    *storage.Store
	Token    string
	MaxBytes int64
	ShareURL string
	Logger   *slog.Logger
}

type Server struct {
	store    *storage.Store
	token    string
	maxBytes int64
	shareURL string
	logger   *slog.Logger
	events   *eventBroker
	limiter  *loginLimiter
	started  time.Time
}

func New(options Options) *Server {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		store:    options.Store,
		token:    options.Token,
		maxBytes: options.MaxBytes,
		shareURL: options.ShareURL,
		logger:   logger,
		events:   newEventBroker(),
		limiter:  newLoginLimiter(5, time.Minute, time.Minute),
		started:  time.Now(),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /api/session", s.handleSession)
	mux.Handle("GET /api/files", s.requireAuth(http.HandlerFunc(s.handleList)))
	mux.Handle("POST /api/files", s.requireAuth(http.HandlerFunc(s.handleUpload)))
	mux.Handle("GET /api/files/{name}", s.requireAuth(http.HandlerFunc(s.handleDownload)))
	mux.Handle("DELETE /api/files/{name}", s.requireAuth(http.HandlerFunc(s.handleDelete)))
	mux.Handle("GET /api/events", s.requireAuth(http.HandlerFunc(s.handleEvents)))
	mux.Handle("GET /api/share", s.requireAuth(http.HandlerFunc(s.handleShare)))
	mux.Handle("GET /api/share/qr", s.requireAuth(http.HandlerFunc(s.handleShareQR)))

	webRoot, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /", http.FileServer(http.FS(webRoot)))

	return s.securityHeaders(s.logRequests(mux))
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	client := clientAddress(r)
	if allowed, retryAfter := s.limiter.allow(client); !allowed {
		writeRateLimit(w, retryAfter)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var request struct {
		Token string `json:"token"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "连接码格式不正确")
		return
	}
	if !s.matchesToken(request.Token) {
		if retryAfter := s.limiter.failure(client); retryAfter > 0 {
			writeRateLimit(w, retryAfter)
			return
		}
		writeError(w, http.StatusUnauthorized, "连接码不正确")
		return
	}
	s.limiter.success(client)

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    s.token,
		Path:     "/api",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ok",
		"uptimeSeconds": int64(time.Since(s.started).Seconds()),
	})
}

func (s *Server) handleList(w http.ResponseWriter, _ *http.Request) {
	files := s.store.List()
	var total int64
	for _, file := range files {
		total += file.Size
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"files":     files,
		"count":     len(files),
		"totalSize": total,
		"maxBytes":  s.maxBytes,
	})
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	maxBody := s.maxBytes + 2*1024*1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	multipartReader, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "请使用 multipart/form-data 上传文件")
		return
	}

	for {
		part, err := multipartReader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "无法读取上传内容")
			return
		}
		if part.FormName() != "file" || part.FileName() == "" {
			part.Close()
			continue
		}

		file, saveErr := s.store.Save(r.Context(), part.FileName(), part, s.maxBytes)
		part.Close()
		if saveErr != nil {
			s.handleStorageError(w, saveErr)
			return
		}
		writeJSON(w, http.StatusCreated, file)
		s.events.publish("created", file.Name)
		return
	}

	writeError(w, http.StatusBadRequest, "没有找到要上传的文件")
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	file, meta, err := s.store.Open(name)
	if err != nil {
		s.handleStorageError(w, err)
		return
	}
	defer file.Close()

	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": meta.Name})
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("X-Content-SHA256", meta.SHA256)
	http.ServeContent(w, r, meta.Name, meta.Modified, file)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Delete(r.PathValue("name")); err != nil {
		s.handleStorageError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	s.events.publish("deleted", r.PathValue("name"))
}

func (s *Server) handleShare(w http.ResponseWriter, r *http.Request) {
	shareURL := s.shareURLFor(r)
	parsed, _ := url.Parse(shareURL)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{
		"url":     shareURL,
		"address": parsed.Host,
	})
}

func (s *Server) handleShareQR(w http.ResponseWriter, r *http.Request) {
	code, err := qr.Encode(s.shareURLFor(r), qr.M)
	if err != nil {
		s.logger.Error("encode share QR code", "error", err)
		writeError(w, http.StatusInternalServerError, "二维码生成失败")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Disposition", "inline; filename=lan-drop-qr.png")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(code.PNG()); err != nil {
		s.logger.Error("write share QR code", "error", err)
	}
}

func (s *Server) shareURLFor(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	current := url.URL{Scheme: scheme, Host: r.Host, Path: "/"}
	query := current.Query()
	query.Set("token", s.token)
	current.RawQuery = query.Encode()

	hostname := strings.TrimSpace(current.Hostname())
	isLocalhost := strings.EqualFold(hostname, "localhost")
	if ip := net.ParseIP(hostname); hostname != "" && !isLocalhost && (ip == nil || !ip.IsLoopback()) {
		return current.String()
	}
	if s.shareURL != "" {
		return s.shareURL
	}
	return current.String()
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := r.Header.Get("X-Share-Token")
		credentialPresented := provided != ""
		if cookie, err := r.Cookie(sessionCookie); err == nil {
			provided = cookie.Value
			credentialPresented = true
		}
		if !s.matchesToken(provided) {
			if credentialPresented {
				client := clientAddress(r)
				if allowed, retryAfter := s.limiter.allow(client); !allowed {
					writeRateLimit(w, retryAfter)
					return
				}
				if retryAfter := s.limiter.failure(client); retryAfter > 0 {
					writeRateLimit(w, retryAfter)
					return
				}
			}
			writeError(w, http.StatusUnauthorized, "需要连接码")
			return
		}
		s.limiter.success(clientAddress(r))
		next.ServeHTTP(w, r)
	})
}

func (s *Server) matchesToken(provided string) bool {
	if len(provided) != len(s.token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) == 1
}

func (s *Server) handleStorageError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrInvalidName):
		writeError(w, http.StatusBadRequest, "文件名不合法")
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, "文件不存在")
	case errors.Is(err, storage.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "文件超过大小限制")
	default:
		s.logger.Error("file operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "文件操作失败")
	}
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := newRequestID()
		w.Header().Set("X-Request-ID", requestID)
		capture := &responseCapture{ResponseWriter: w}
		r = r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, requestID))
		next.ServeHTTP(capture, r)
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
			s.logger.Info("request",
				"request_id", requestID,
				"method", r.Method,
				"path", safePath(r.URL),
				"status", capture.statusCode(),
				"bytes", capture.bytes,
				"duration", time.Since(started),
			)
		}
	})
}

type requestIDContextKey struct{}

type responseCapture struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *responseCapture) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseCapture) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}

func (w *responseCapture) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *responseCapture) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func newRequestID() string {
	var value [8]byte
	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}
	return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
}

func safePath(u *url.URL) string {
	if strings.HasPrefix(u.Path, "/api/files/") {
		return "/api/files/{name}"
	}
	return u.Path
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("write JSON response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{
		"error":   http.StatusText(status),
		"message": message,
	})
}

func (s *Server) String() string {
	return fmt.Sprintf("LAN Drop (%s)", s.store.Root())
}
