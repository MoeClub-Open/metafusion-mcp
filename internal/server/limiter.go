package server

import (
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

type bucket struct {
	At    time.Time
	Count int
}
type limiter struct {
	mu    sync.Mutex
	items map[string]bucket
	cfg   RateLimitConfig
}

func defaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		Configured: true,
		ReadRPM:    defaultRateLimitRPM,
		WriteRPM:   defaultRateLimitRPM,
		OAuthRPM:   defaultRateLimitRPM,
	}
}

func newLimiter() *limiter { return newLimiterWithConfig(defaultRateLimitConfig()) }

func newLimiterWithConfig(cfg RateLimitConfig) *limiter {
	if !cfg.Configured {
		cfg = defaultRateLimitConfig()
	}
	return &limiter{items: map[string]bucket{}, cfg: cfg}
}

func (l *limiter) allowKey(key string, limit int) bool {
	if limit <= 0 {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.items) > 65536 {
		for k, b := range l.items {
			if now.Sub(b.At) > time.Minute {
				delete(l.items, k)
			}
		}
		if len(l.items) > 65536 {
			return false
		}
	}
	b := l.items[key]
	if now.Sub(b.At) >= time.Minute {
		b = bucket{At: now}
	}
	b.Count++
	l.items[key] = b
	return b.Count <= limit
}

func (l *limiter) Allow(r *http.Request) bool {
	// RemoteAddr is the actual trusted peer. Never accept client-supplied proxy IP headers.
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	key := ip
	limit := l.cfg.ReadRPM
	if bearer := r.Header.Get("Authorization"); bearer != "" {
		key = hash(bearer)
	}
	if strings.HasPrefix(r.URL.Path, "/mcp/oauth/") {
		key = "oauth:" + ip
		limit = l.cfg.OAuthRPM
	}
	return l.allowKey(key, limit)
}

func (l *limiter) AllowAPI(p Principal, method, path string) bool {
	if slices.Contains(p.Groups, "admin") {
		return true
	}
	kind := "write"
	limit := l.cfg.WriteRPM
	if readOperation(strings.ToUpper(method), path) {
		kind = "read"
		limit = l.cfg.ReadRPM
	}
	return l.allowKey(hash(p.Grant.UserID+":"+kind), limit)
}
