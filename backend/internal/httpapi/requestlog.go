package httpapi

import (
	"log/slog"
	"net/http"
	"time"
)

// WithRequestLog wraps an http.Handler to emit one structured log line per
// request (method, path, status, duration, remote). It logs with the request
// context so OTel trace/span ids attach when present. Server errors (5xx) log
// at error level, client errors (4xx) at warn, the rest at info.
func WithRequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)

		level := slog.LevelInfo
		switch {
		case sw.status >= 500:
			level = slog.LevelError
		case sw.status >= 400:
			level = slog.LevelWarn
		}
		slog.Default().LogAttrs(r.Context(), level, "http request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", sw.status),
			slog.Duration("duration", time.Since(start)),
			slog.String("remote", r.RemoteAddr),
			slog.String("user_agent", r.UserAgent()),
		)
	})
}

// statusWriter records the status code written by the handler. Handlers that
// never call WriteHeader implicitly write 200, which is the initial value.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the underlying writer's optional
// interfaces (Flusher, Hijacker, ...).
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
