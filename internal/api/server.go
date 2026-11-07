package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/Brohammad/Arden/internal/store"
)

type server struct {
	log   *slog.Logger
	store *store.Store
}

// NewHandler returns the HTTP API.
func NewHandler(log *slog.Logger, st *store.Store) http.Handler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	s := &server{log: log, store: st}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)
	mux.HandleFunc("POST /v1/jobs", s.createJob)
	mux.HandleFunc("GET /v1/jobs", s.listJobs)
	mux.HandleFunc("GET /v1/jobs/{id}", s.getJob)
	mux.HandleFunc("POST /v1/jobs/{id}/cancel", s.cancelJob)
	return s.logging(mux)
}

func (s *server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) readyz(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		s.log.Error("readiness check failed", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		status := sw.status
		if status == 0 {
			status = http.StatusOK
		}
		s.log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration", time.Since(start),
		)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}
