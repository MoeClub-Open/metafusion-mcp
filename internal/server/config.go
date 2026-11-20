package server

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const (
	defaultRateLimitRPM       = 180
	defaultFileConcurrency    = 64
	defaultHTTPMaxIdleConns   = 1024
	defaultHTTPMaxIdlePerHost = 256
	defaultHTTPTimeoutSeconds = 45
)

type RateLimitConfig struct {
	Configured bool
	ReadRPM    int
	WriteRPM   int
	OAuthRPM   int
}

type Config struct {
	PublicURL, APIURL, AuthURL, DatabaseURL, Listen string
	Key                                             []byte
	AllowedOrigins                                  []string
	SkillsRevision, DocsRevision                    string
	ObjectOrigins                                   []string
	SearchProvider, SearchURL, SearchAPIKey         string
	RateLimit                                       RateLimitConfig
	FileConcurrency                                 int
	FileConcurrencyConfigured                       bool
	HTTPMaxIdleConns                                int
	HTTPMaxIdleConnsPerHost                         int
	HTTPTimeoutSeconds                              int
}

func LoadConfig() (Config, error) {
	c := Config{PublicURL: strings.TrimRight(os.Getenv("MCP_PUBLIC_URL"), "/"), APIURL: strings.TrimRight(os.Getenv("MCP_API_URL"), "/"), AuthURL: strings.TrimRight(os.Getenv("MCP_AUTH_URL"), "/"), DatabaseURL: os.Getenv("MCP_DATABASE_URL"), Listen: os.Getenv("MCP_LISTEN")}
	c.SkillsRevision = os.Getenv("MCP_SKILLS_REVISION")
	c.DocsRevision = os.Getenv("MCP_DOCS_REVISION")
	c.SearchProvider = strings.ToLower(strings.TrimSpace(os.Getenv("MCP_SEARCH_PROVIDER")))
	c.SearchURL = strings.TrimRight(strings.TrimSpace(os.Getenv("MCP_SEARCH_URL")), "/")
	c.SearchAPIKey = os.Getenv("MCP_SEARCH_API_KEY")
	if c.SearchProvider == "" {
		c.SearchProvider = "duckduckgo"
	}
	switch c.SearchProvider {
	case "bing":
		if c.SearchURL == "" {
			c.SearchURL = "https://www.bing.com/search"
		}
	case "duckduckgo":
		if c.SearchURL == "" {
			c.SearchURL = "https://html.duckduckgo.com/html/"
		}
	case "brave":
		if c.SearchURL == "" {
			c.SearchURL = "https://api.search.brave.com/res/v1/web/search"
		}
		if c.SearchAPIKey == "" {
			return c, errors.New("MCP_SEARCH_API_KEY is required for Brave search")
		}
	case "searxng":
		if c.SearchURL == "" {
			return c, errors.New("MCP_SEARCH_URL is required for SearXNG search")
		}
	default:
		return c, errors.New("MCP_SEARCH_PROVIDER must be duckduckgo, bing, brave or searxng")
	}
	if u, err := url.Parse(c.SearchURL); err != nil || !validWebURL(u) {
		return c, errors.New("MCP_SEARCH_URL must be a public HTTP(S) URL on the default port without credentials or fragments")
	}
	for _, origin := range strings.Split(os.Getenv("MCP_OBJECT_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			u, e := url.Parse(origin)
			if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
				return c, errors.New("MCP_OBJECT_ORIGINS must be explicitly trusted HTTP(S) origins")
			}
			c.ObjectOrigins = append(c.ObjectOrigins, origin)
		}
	}
	if c.Listen == "" {
		c.Listen = ":8084"
	}
	if c.AuthURL == "" {
		c.AuthURL = c.APIURL
	}
	for _, base := range []string{c.PublicURL, c.APIURL, c.AuthURL} {
		u, err := url.Parse(base)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return c, errors.New("MCP URLs must be HTTP(S) origins without paths or credentials")
		}
	}
	u, _ := url.Parse(c.PublicURL)
	if u.Scheme != "https" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return c, errors.New("MCP_PUBLIC_URL must use HTTPS outside loopback development")
	}
	c.Key, _ = base64.StdEncoding.DecodeString(os.Getenv("MCP_ENCRYPTION_KEY"))
	if len(c.Key) != 32 {
		return c, errors.New("MCP_ENCRYPTION_KEY must encode exactly 32 random bytes")
	}
	if c.DatabaseURL == "" {
		return c, errors.New("MCP_DATABASE_URL is required")
	}
	c.AllowedOrigins = []string{c.PublicURL}
	for _, origin := range strings.Split(os.Getenv("MCP_ALLOWED_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			c.AllowedOrigins = append(c.AllowedOrigins, origin)
		}
	}
	var err error
	if c.RateLimit.ReadRPM, err = envNonNegInt("MCP_RATE_LIMIT_READ_RPM", defaultRateLimitRPM); err != nil {
		return c, err
	}
	if c.RateLimit.WriteRPM, err = envNonNegInt("MCP_RATE_LIMIT_WRITE_RPM", defaultRateLimitRPM); err != nil {
		return c, err
	}
	if c.RateLimit.OAuthRPM, err = envNonNegInt("MCP_RATE_LIMIT_OAUTH_RPM", defaultRateLimitRPM); err != nil {
		return c, err
	}
	c.RateLimit.Configured = true
	if c.FileConcurrency, err = envNonNegInt("MCP_FILE_CONCURRENCY", defaultFileConcurrency); err != nil {
		return c, err
	}
	c.FileConcurrencyConfigured = true
	if c.HTTPMaxIdleConns, err = envPosInt("MCP_HTTP_MAX_IDLE_CONNS", defaultHTTPMaxIdleConns); err != nil {
		return c, err
	}
	if c.HTTPMaxIdleConnsPerHost, err = envPosInt("MCP_HTTP_MAX_IDLE_CONNS_PER_HOST", defaultHTTPMaxIdlePerHost); err != nil {
		return c, err
	}
	if c.HTTPTimeoutSeconds, err = envPosInt("MCP_HTTP_TIMEOUT_SECONDS", defaultHTTPTimeoutSeconds); err != nil {
		return c, err
	}
	return c, nil
}

func envNonNegInt(name string, def int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return n, nil
}

func envPosInt(name string, def int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return n, nil
}
