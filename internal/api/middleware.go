// Package api provides HTTP handlers, middleware, and route registration.
package api

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Middleware wraps an http.Handler to add cross-cutting behavior.
type Middleware func(http.Handler) http.Handler

// contextKey is an unexported type for context keys in this package.
type contextKey string

const requestIDKey contextKey = "request_id"

// Chain applies middleware to h in order (first middleware is outermost).
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// WithRequestID generates a UUID v4 request ID, injects it into the request
// context, and sets the X-Request-ID response header.
func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// WithRecover catches panics, logs them via slog, and writes a 500 response.
// If the handler already started writing a response before panicking, the
// recovered 500 is suppressed (headers can only be written once) and only the
// log record is emitted.
func WithRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw, ok := w.(*responseWriter)
		if !ok {
			rw = &responseWriter{ResponseWriter: w, status: http.StatusOK}
		}
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered", "panic", rec, "path", r.URL.Path)
				if !rw.wrote {
					writeError(rw, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error", false)
				}
			}
		}()
		next.ServeHTTP(rw, r)
	})
}

// WithCORS adds permissive CORS headers for localhost development.
// The server only binds to 127.0.0.1, but wildcard CORS would still enable
// DNS-rebinding attacks. WithHostAllowlist (applied upstream) blocks those.
func WithCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// WithHostAllowlist rejects requests whose Host header is not a localhost
// variant. This defends against DNS-rebinding attacks where a browser visits a
// malicious site that resolves to 127.0.0.1 — the allowlist ensures even then
// that the Host header fails the check.
func WithHostAllowlist(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			// No port; use raw Host.
			host = r.Host
		}
		switch host {
		case "localhost", "127.0.0.1", "[::1]", "::1":
			next.ServeHTTP(w, r)
		default:
			writeError(w, http.StatusForbidden, "FORBIDDEN_HOST", "host not allowed", false)
		}
	})
}

// WithRequestLog returns a middleware that logs each request using the provided logger.
func WithRequestLog(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)
			rid, _ := r.Context().Value(requestIDKey).(string)
			uri := r.URL.Path
			if r.URL.RawQuery != "" {
				uri = uri + "?" + r.URL.RawQuery
			}
			logger.Info(r.Method+" "+uri,
				"method", r.Method,
				"path", r.URL.Path,
				"query", r.URL.RawQuery,
				"status", rw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote_addr", r.RemoteAddr,
				"request_id", rid,
			)
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture the status code and track
// whether headers have been written — preventing double-WriteHeader errors when
// a panic recovery path needs to emit a response after the handler partially
// wrote one.
type responseWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (rw *responseWriter) WriteHeader(status int) {
	if rw.wrote {
		return
	}
	rw.wrote = true
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wrote {
		rw.wrote = true
	}
	return rw.ResponseWriter.Write(b)
}

func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// newRequestID generates a UUID v4 string using crypto/rand.
func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "unknown"
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
