package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Expected API errors (validation, auth, not-found, conflicts, throttling,
// temporary unavailability) are returned as ordinary tool results with
// ok:false so agents can branch on status/error without parsing exception
// text. Only transport failures and genuine internal upstream failures keep
// IsError, which MCP clients surface as a tool execution failure.

// expectedError builds the uniform structured error object.
func expectedError(status int, code, message string, details any, retryAfter any) *mcp.CallToolResult {
	out := map[string]any{"ok": false, "status": status, "error": code, "message": message, "details": details, "retry_after": retryAfter, "request_id": nil}
	return result(out, false)
}

// internalFailure reports whether an upstream status is a real server fault
// rather than an expected, documented API error.
func internalFailure(status int) bool {
	return status >= 500 && status != http.StatusServiceUnavailable
}

func upstreamResult(status int, h http.Header, headers map[string]string, data any) *mcp.CallToolResult {
	if status < 400 {
		return result(map[string]any{"ok": true, "status": status, "headers": headers, "data": data}, false)
	}
	body, _ := data.(map[string]any)
	code, message := "", ""
	var details any
	if body != nil {
		code = firstString(body, "error", "code")
		message = firstString(body, "message", "error_description", "detail")
		if d, ok := body["details"]; ok {
			details = d
		} else {
			rest := map[string]any{}
			for k, v := range body {
				switch k {
				case "error", "code", "message", "error_description", "detail", "request_id", "retry_after":
				default:
					rest[k] = v
				}
			}
			if len(rest) > 0 {
				details = rest
			}
		}
	}
	if code == "" {
		code = "http_" + strconv.Itoa(status)
	}
	if message == "" {
		message = http.StatusText(status)
	}
	var retry any
	if v, ok := parseRetryAfter(h.Get("Retry-After")); ok {
		retry = v
	} else if body != nil {
		if v, ok := body["retry_after"].(float64); ok {
			retry = int(v)
		}
	}
	var requestID any
	for _, k := range []string{"X-Request-ID", "X-Request-Id", "Request-Id"} {
		if v := h.Get(k); v != "" {
			requestID = v
			break
		}
	}
	if requestID == nil && body != nil {
		if v, ok := body["request_id"].(string); ok && v != "" {
			requestID = v
		}
	}
	out := map[string]any{"ok": false, "status": status, "error": code, "message": message, "details": details, "retry_after": retry, "request_id": requestID, "headers": headers, "data": data}
	return result(out, internalFailure(status))
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// parseRetryAfter accepts delta-seconds or an HTTP-date and returns seconds.
func parseRetryAfter(v string) (int, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if n, e := strconv.Atoi(v); e == nil && n >= 0 {
		return n, true
	}
	if t, e := http.ParseTime(v); e == nil {
		return max(0, int(time.Until(t).Round(time.Second).Seconds())), true
	}
	return 0, false
}

// failedResult is true for both exceptions and structured ok:false results.
func failedResult(r *mcp.CallToolResult) bool {
	if r == nil || r.IsError {
		return true
	}
	m, ok := r.StructuredContent.(map[string]any)
	return ok && m["ok"] == false
}
