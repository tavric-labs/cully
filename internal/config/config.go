package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode"
)

type Settings struct {
	Address      string
	DatabaseURL  string
	DataAPIURL   string
	DataAPIToken string
	Issuer       string
	Resource     string
	JWKSURL      string
	MCPPath      string
	AuthMode     string
	Owner        string
	Mem0URL      string
	Mem0APIKey   string
	MaxConns     int32
}

func value(key, defaultValue string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return defaultValue
}
func Load(data bool) (Settings, error) {
	port := "8080"
	if data {
		port = "8083"
	}
	port = value("CULLY_PORT", port)
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return Settings{}, fmt.Errorf("invalid CULLY_PORT")
	}
	s := Settings{Address: value("CULLY_HOST", "0.0.0.0") + ":" + port, DatabaseURL: os.Getenv("CULLY_DATABASE_URL"), DataAPIURL: os.Getenv("CULLY_DATA_API_URL"), DataAPIToken: os.Getenv("CULLY_DATA_API_TOKEN"), MCPPath: value("MCP_PATH", "/mcp"), Mem0URL: os.Getenv("CULLY_MEM0_URL"), Mem0APIKey: os.Getenv("CULLY_MEM0_API_KEY"), MaxConns: 8}
	if !strings.HasPrefix(s.MCPPath, "/") || strings.ContainsAny(s.MCPPath, " {}?#") {
		return s, fmt.Errorf("invalid MCP_PATH")
	}
	if v := os.Getenv("CULLY_DB_MAX_CONNS"); v != "" {
		n, e := strconv.ParseInt(v, 10, 32)
		if e != nil || n < 2 || n > 100 {
			return s, fmt.Errorf("CULLY_DB_MAX_CONNS must be 2..100")
		}
		s.MaxConns = int32(n)
	}
	if data && s.DatabaseURL == "" {
		return s, fmt.Errorf("CULLY_DATABASE_URL is required")
	}
	return s, nil
}

// LoadMCP selects the authentication boundary before any identity provider is contacted.
func LoadMCP(oauthFlag bool) (Settings, error) {
	s, err := Load(false)
	if err != nil {
		return s, err
	}
	s.AuthMode = value("CULLY_MCP_AUTH_MODE", "none")
	if s.AuthMode != "none" && s.AuthMode != "oauth" {
		return s, fmt.Errorf("CULLY_MCP_AUTH_MODE must be none or oauth")
	}
	if oauthFlag {
		s.AuthMode = "oauth"
	}
	if s.AuthMode == "none" {
		s.Owner = os.Getenv("CULLY_MCP_OWNER")
		if strings.TrimSpace(s.Owner) == "" || len(s.Owner) > 512 || strings.TrimSpace(s.Owner) != s.Owner || strings.IndexFunc(s.Owner, unicode.IsControl) >= 0 {
			return s, fmt.Errorf("CULLY_MCP_OWNER is required in no-OAuth mode (1–512 bytes, no surrounding whitespace or control characters)")
		}
		if strings.TrimSpace(os.Getenv("CULLY_HOST")) == "" {
			s.Address = "127.0.0.1:" + value("CULLY_PORT", "8080")
		}
		return s, nil
	}
	if os.Getenv("CULLY_MCP_OWNER") != "" {
		return s, fmt.Errorf("CULLY_MCP_OWNER must be unset in OAuth mode")
	}
	s.Issuer = strings.TrimRight(value("MCP_AUTH_ISSUER", value("CULLY_AUTH_ISSUER", "")), "/")
	s.Resource = strings.TrimRight(value("MCP_AUTH_RESOURCE", value("CULLY_AUTH_RESOURCE", "")), "/")
	s.JWKSURL = os.Getenv("CULLY_JWKS_URL")
	for key, raw := range map[string]string{"MCP_AUTH_ISSUER or CULLY_AUTH_ISSUER": s.Issuer, "MCP_AUTH_RESOURCE or CULLY_AUTH_RESOURCE": s.Resource, "CULLY_JWKS_URL": s.JWKSURL} {
		u, e := url.Parse(raw)
		if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return s, fmt.Errorf("%s must be an absolute HTTP(S) URL without credentials, query or fragment in OAuth mode", key)
		}
	}
	return s, nil
}
