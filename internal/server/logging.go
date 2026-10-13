package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type toolNameKey struct{}

// toolNameMiddleware records the called tool name so upstream call logs can
// be attributed to it.
func toolNameMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if r, ok := req.(*mcp.CallToolRequest); ok && r.Params != nil {
			ctx = context.WithValue(ctx, toolNameKey{}, r.Params.Name)
		}
		return next(ctx, method, req)
	}
}

func toolName(ctx context.Context) string {
	n, _ := ctx.Value(toolNameKey{}).(string)
	return n
}

func newRequestID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "mcp-" + time.Now().UTC().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(b[:])
}

// logUpstream writes one record per upstream call. Only routing metadata is
// logged: never query values, bodies, headers or credentials.
func (s *Server) logUpstream(ctx context.Context, requestID, method, path string, status int, errCode string, started time.Time) {
	if s.Log == nil {
		return
	}
	level := slog.LevelInfo
	if status == 0 || status >= 500 {
		level = slog.LevelError
	} else if status >= 400 {
		level = slog.LevelWarn
	}
	attrs := []slog.Attr{
		slog.String("event", "upstream_call"),
		slog.String("request_id", requestID),
		slog.String("tool", toolName(ctx)),
		slog.String("method", method),
		slog.String("path", path),
		slog.Int("status", status),
		slog.String("error", errCode),
		slog.Int64("duration_ms", time.Since(started).Milliseconds()),
	}
	if p, ok := currentPrincipal(ctx); ok {
		attrs = append(attrs, slog.String("user_id", p.Grant.UserID))
	}
	s.Log.LogAttrs(ctx, level, "upstream call", attrs...)
}
