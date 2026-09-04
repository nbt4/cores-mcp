package httpx

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

func Recover(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if value := recover(); value != nil {
				logger.Error("http panic", "path", r.URL.Path, "panic", value, "stack", string(debug.Stack()))
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func SecurityHeaders(publicURL string, next http.Handler) http.Handler {
	policy := contentSecurityPolicy(publicURL)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", policy)
		next.ServeHTTP(w, r)
	})
}

func contentSecurityPolicy(publicURL string) string {
	formAction := "'self'"
	if parsed, err := url.Parse(publicURL); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" {
		// Some browsers do not consistently match 'self' for a form served
		// through a reverse proxy. Add the canonical public origin explicitly.
		formAction += " " + parsed.Scheme + "://" + parsed.Host
	}
	return fmt.Sprintf("default-src 'none'; style-src 'self' https://fonts.googleapis.com; font-src https://fonts.gstatic.com; img-src 'self'; form-action %s; frame-ancestors 'none'; base-uri 'none'", formAction)
}

func ValidateOrigin(publicURL string, allowed map[string]struct{}, next http.Handler) http.Handler {
	base, _ := url.Parse(publicURL)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimRight(strings.TrimSpace(r.Header.Get("Origin")), "/")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		parsed, err := url.Parse(origin)
		_, explicitlyAllowed := allowed[origin]
		if err != nil || (!explicitlyAllowed && !sameOrigin(parsed, base)) {
			http.Error(w, "invalid origin", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(a.Scheme+"://"+a.Host)), []byte(strings.ToLower(b.Scheme+"://"+b.Host))) == 1
}

type rateEntry struct {
	window time.Time
	count  int
}

type RateLimiter struct {
	mu         sync.Mutex
	limit      int
	entries    map[string]rateEntry
	trustProxy bool
}

func NewRateLimiter(limit int, trustProxy bool) *RateLimiter {
	return &RateLimiter{limit: limit, entries: make(map[string]rateEntry), trustProxy: trustProxy}
}

func (l *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := clientIP(r, l.trustProxy)
		now := time.Now()
		l.mu.Lock()
		entry := l.entries[key]
		if entry.window.IsZero() || now.Sub(entry.window) >= time.Minute {
			entry = rateEntry{window: now}
		}
		entry.count++
		l.entries[key] = entry
		allowed := entry.count <= l.limit
		if len(l.entries) > 10000 {
			for candidate, value := range l.entries {
				if now.Sub(value.window) > 2*time.Minute {
					delete(l.entries, candidate)
				}
			}
		}
		l.mu.Unlock()
		if !allowed {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); forwarded != "" {
			return forwarded
		}
		if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); real != "" {
			return real
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
