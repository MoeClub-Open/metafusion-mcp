package server

import (
	"embed"
	"net/http"
	"strings"
)

//go:embed web/*
var web embed.FS

// Keep one management UI; the standalone page is only for OAuth consent.
func (s *Server) managePage(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, s.Config.PublicURL+"/developer?tab=mcp", http.StatusSeeOther)
}

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	b, e := web.ReadFile("web/index.html")
	if e != nil {
		writeError(w, 500, "page_unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	_, _ = w.Write(b)
}
func (s *Server) asset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if !strings.Contains(name, ".") || strings.ContainsAny(name, "/\\") {
		writeError(w, 404, "not_found")
		return
	}
	b, e := web.ReadFile("web/" + name)
	if e != nil {
		writeError(w, 404, "not_found")
		return
	}
	switch {
	case strings.HasSuffix(name, ".js"):
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	case strings.HasSuffix(name, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case strings.HasSuffix(name, ".json"):
		w.Header().Set("Content-Type", "application/json")
	default:
		writeError(w, 404, "not_found")
		return
	}
	_, _ = w.Write(b)
}
