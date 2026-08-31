package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type contextKey string

const requestIDKey contextKey = "request-id"

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' https://media.licdn.com https://*.licdn.com; style-src 'self'; script-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func recoverer(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("request panic", "request_id", requestIDFrom(r.Context()), "error", recovered)
				writeError(w, http.StatusInternalServerError, "internal_error", "The server could not process the request.", requestIDFrom(r.Context()))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func accessLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(recorder, r)
		logger.Info("request completed",
			"request_id", requestIDFrom(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func withTimeout(timeout time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requireAPIKey(expected string, next http.Handler) http.Handler {
	if expected == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := r.Header.Get("X-API-Key")
		if len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "A valid X-API-Key header is required.", requestIDFrom(r.Context()))
			return
		}
		next.ServeHTTP(w, r)
	})
}

type clientLimiter struct {
	mu      sync.Mutex
	limit   rate.Limit
	burst   int
	clients map[string]*clientLimit
}

type clientLimit struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func newClientLimiter(requestsPerMinute, burst int) *clientLimiter {
	return &clientLimiter{
		limit:   rate.Limit(float64(requestsPerMinute) / 60),
		burst:   burst,
		clients: make(map[string]*clientLimit),
	}
}

func (l *clientLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := clientAddress(r)
		if !l.allow(key) {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many requests. Try again later.", requestIDFrom(r.Context()))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (l *clientLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	client, ok := l.clients[key]
	if !ok {
		client = &clientLimit{
			limiter: rate.NewLimiter(l.limit, l.burst),
		}
		l.clients[key] = client
	}
	client.lastSeen = now
	if len(l.clients) > 1024 {
		for clientKey, item := range l.clients {
			if now.Sub(item.lastSeen) > 10*time.Minute {
				delete(l.clients, clientKey)
			}
		}
		for clientKey := range l.clients {
			if len(l.clients) <= 1024 {
				break
			}
			if clientKey != key {
				delete(l.clients, clientKey)
			}
		}
	}
	return client.limiter.Allow()
}

func clientAddress(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if value := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(value) != nil {
			return value
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

func requestIDFrom(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}

func newRequestID() string {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(value)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
