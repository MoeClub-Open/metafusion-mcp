package server

import (
	"encoding/base64"
	"errors"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	PublicURL, APIURL, AuthURL, DatabaseURL, Listen string
	Key                                             []byte
	AllowedOrigins                                  []string
	SkillsRevision, DocsRevision                    string
	ObjectOrigins                                   []string
}

func LoadConfig() (Config, error) {
	c := Config{PublicURL: strings.TrimRight(os.Getenv("MCP_PUBLIC_URL"), "/"), APIURL: strings.TrimRight(os.Getenv("MCP_API_URL"), "/"), AuthURL: strings.TrimRight(os.Getenv("MCP_AUTH_URL"), "/"), DatabaseURL: os.Getenv("MCP_DATABASE_URL"), Listen: os.Getenv("MCP_LISTEN")}
	c.SkillsRevision = os.Getenv("MCP_SKILLS_REVISION")
	c.DocsRevision = os.Getenv("MCP_DOCS_REVISION")
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
	return c, nil
}
