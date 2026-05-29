package middleware

import (
	"log/slog"
	"net/http"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// RealIP sets r.RemoteAddr from the X-Real-IP header, which the trusted nginx
// front-end overwrites with the actual client IP (proxy_set_header X-Real-IP
// $remote_addr). We deliberately do NOT trust X-Forwarded-For: nginx appends to
// it, so a client-supplied leftmost value would win and allow IP spoofing of the
// rate limiter and blacklist — which is why chi's RealIP middleware is deprecated.
// When the header is absent (e.g. local dev without nginx) the direct connection
// address is left untouched.
func RealIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ip := r.Header.Get("X-Real-IP"); ip != "" {
			r.RemoteAddr = ip
		}
		next.ServeHTTP(w, r)
	})
}

// RequestLogger logs one structured (slog) line per request, so request logs
// share the same JSON format as the rest of the application's logging.
// It must run after chi's RequestID and RealIP middleware so those values
// are populated.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()

		defer func() {
			slog.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"remote_ip", r.RemoteAddr,
				"request_id", chimiddleware.GetReqID(r.Context()),
			)
		}()

		next.ServeHTTP(ww, r)
	})
}
