package server

import (
	"net"
	"net/http"
	"net/url"
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
}

func newLimiter() *limiter { return &limiter{items: map[string]bucket{}} }
func (l *limiter) Allow(r *http.Request) bool {
	// RemoteAddr is the actual trusted peer. Never accept client-supplied proxy IP headers.
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	key := ip
	limit := 180
	if bearer := r.Header.Get("Authorization"); bearer != "" {
		key = hash(bearer)
	}
	if strings.HasPrefix(r.URL.Path, "/mcp/oauth/") {
		key = "oauth:" + ip
		limit = 180
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

func (l *limiter) AllowAPI(p Principal, method, path string) bool {
	if slices.Contains(p.Groups, "admin") {
		return true
	}
	kind := "write"
	if readOperation(strings.ToUpper(method), path) {
		kind = "read"
	}
	r := &http.Request{RemoteAddr: p.Grant.UserID + ":0", Header: make(http.Header), URL: &url.URL{Path: "/mcp"}}
	r.Header.Set("Authorization", p.Grant.UserID+":"+kind)
	return l.Allow(r)
}
