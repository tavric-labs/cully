package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

var selfHostFields = map[string]string{
	"CULLY_DB_PASSWORD":      "db_password",
	"CULLY_DATA_API_TOKEN":   "data_api_token",
	"CULLY_MCP_OWNER":        "mcp_owner",
	"CULLY_MEM0_DB_PASSWORD": "mem0_db_password",
	"CULLY_MEM0_API_KEY":     "mem0_api_key",
	"CULLY_MEM0_JWT_SECRET":  "mem0_jwt_secret",
}

func runSelfHostCredentials(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cully _internal self-hosted-credentials ensure|export|oauth-env")
	}
	configPath, err := selfHostConfigPath()
	if err != nil {
		return err
	}
	switch args[0] {
	case "ensure":
		if len(args) != 1 {
			return fmt.Errorf("usage: cully _internal self-hosted-credentials ensure")
		}
		if _, err := ensureSelfHostCredentials(configPath, ".env"); err != nil {
			return err
		}
		fmt.Printf("Self-hosted credentials ready in %s\n", configPath)
	case "export":
		if len(args) != 1 {
			return fmt.Errorf("usage: cully _internal self-hosted-credentials export")
		}
		values, err := loadSelfHostCredentials(configPath)
		if err != nil {
			return err
		}
		printSelfHostExports(values)
	case "oauth-env":
		if len(args) != 2 || args[1] != "mcp-auth" {
			return fmt.Errorf("usage: cully _internal self-hosted-credentials oauth-env mcp-auth")
		}
		values, err := selfHostOAuthValues(".")
		if err != nil {
			return err
		}
		for _, key := range []string{"CULLY_AUTH_ISSUER", "CULLY_AUTH_RESOURCE", "CULLY_JWKS_URL", "CULLY_MCP_AUTH_CONNECTOR"} {
			fmt.Printf("export %s=%s\n", key, shellQuote(values[key]))
		}
	default:
		return fmt.Errorf("unknown self-hosted credential action %q", args[0])
	}
	return nil
}

func selfHostConfigPath() (string, error) {
	if path := os.Getenv("CULLY_CONFIG_PATH"); path != "" {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cully", "config.json"), nil
}

func readSelfHostEnv(path string) (map[string]string, error) {
	values := make(map[string]string)
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return values, nil
	}
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), "\"'")
	}
	return values, nil
}

func readSelfHostDocument(path string) (map[string]json.RawMessage, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return make(map[string]json.RawMessage), nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing non-regular config: %s", path)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(contents, &document); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if document == nil {
		return nil, fmt.Errorf("config.json must contain a JSON object")
	}
	return document, nil
}

func selfHostSection(document map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	section := make(map[string]json.RawMessage)
	if raw, ok := document["self_hosted"]; ok {
		if err := json.Unmarshal(raw, &section); err != nil || section == nil {
			return nil, fmt.Errorf("self_hosted must be a JSON object")
		}
	}
	return section, nil
}

func selfHostString(section map[string]json.RawMessage, field string) (string, error) {
	raw, ok := section[field]
	if !ok || string(raw) == "null" {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("self_hosted %s must be a string", field)
	}
	return value, nil
}

func privateSelfHostDirectory(path string) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("refusing non-directory config path: %s", directory)
	}
	if filepath.Base(directory) == ".cully" {
		return os.Chmod(directory, 0o700)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("config directory must be private: %s", directory)
	}
	return nil
}

func saveSelfHostDocument(path string, document map[string]json.RawMessage) error {
	if err := privateSelfHostDirectory(path); err != nil {
		return err
	}
	contents, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".config-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(append(contents, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func existingSelfHostSecret(value string) bool {
	return value != "" && !strings.HasPrefix(value, "replace-with-")
}

func newSelfHostSecret() (string, error) {
	bytes := make([]byte, 36)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func ensureSelfHostCredentials(path, envPath string) (map[string]string, error) {
	document, err := readSelfHostDocument(path)
	if err != nil {
		return nil, err
	}
	if err := privateSelfHostDirectory(path); err != nil {
		return nil, err
	}
	section, err := selfHostSection(document)
	if err != nil {
		return nil, err
	}
	env, err := readSelfHostEnv(envPath)
	if err != nil {
		return nil, err
	}
	values := make(map[string]string)
	for _, field := range []string{"db_name", "db_user"} {
		value, err := selfHostString(section, field)
		if err != nil {
			return nil, err
		}
		if _, ok := section[field]; !ok {
			value = env["CULLY_"+strings.ToUpper(field)]
			if value == "" {
				value = "cully"
			}
		}
		if value == "" {
			return nil, fmt.Errorf("self_hosted %s must not be empty", field)
		}
		values[field] = value
	}
	for key, field := range selfHostFields {
		value, err := selfHostString(section, field)
		if err != nil {
			return nil, err
		}
		if value == "" {
			value = env[key]
			if field == "db_password" && !existingSelfHostSecret(value) {
				if parsed, err := url.Parse(env["CULLY_DATABASE_URL"]); err == nil && parsed.User != nil {
					value, _ = parsed.User.Password()
				}
			}
			if !existingSelfHostSecret(value) {
				value, err = newSelfHostSecret()
				if err != nil {
					return nil, err
				}
				if field == "mcp_owner" {
					value = "local-" + value
				}
			}
		}
		values[field] = value
	}
	changed := false
	for field, value := range values {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		if string(section[field]) != string(raw) {
			section[field] = raw
			changed = true
		}
	}
	if _, ok := document["self_hosted"]; !ok || changed {
		raw, err := json.Marshal(section)
		if err != nil {
			return nil, err
		}
		document["self_hosted"] = raw
		if err := saveSelfHostDocument(path, document); err != nil {
			return nil, err
		}
	} else if err := os.Chmod(path, 0o600); err != nil {
		return nil, err
	}
	return values, nil
}

func loadSelfHostCredentials(path string) (map[string]string, error) {
	document, err := readSelfHostDocument(path)
	if err != nil {
		return nil, err
	}
	section, err := selfHostSection(document)
	if err != nil {
		return nil, err
	}
	values := make(map[string]string)
	for _, field := range []string{"db_name", "db_user", "db_password", "data_api_token", "mcp_owner", "mem0_db_password", "mem0_api_key", "mem0_jwt_secret"} {
		values[field], err = selfHostString(section, field)
		if err != nil || values[field] == "" {
			return nil, fmt.Errorf("run cully setup to create the self-hosted credentials")
		}
	}
	return values, nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func printSelfHostExports(values map[string]string) {
	for _, key := range []string{"CULLY_DB_PASSWORD", "CULLY_DATA_API_TOKEN", "CULLY_MCP_OWNER", "CULLY_MEM0_DB_PASSWORD", "CULLY_MEM0_API_KEY", "CULLY_MEM0_JWT_SECRET"} {
		fmt.Printf("export %s=%s\n", key, shellQuote(values[selfHostFields[key]]))
	}
	for _, pair := range [][2]string{{"CULLY_DB_NAME", "db_name"}, {"CULLY_DB_USER", "db_user"}} {
		fmt.Printf("export %s=%s\n", pair[0], shellQuote(values[pair[1]]))
	}
	connection := "postgres://" + url.UserPassword(values["db_user"], values["db_password"]).String() + "@db:5432/" + url.PathEscape(values["db_name"])
	fmt.Printf("export CULLY_DATABASE_URL=%s\n", shellQuote(connection))
}

func selfHostOAuthValues(directory string) (map[string]string, error) {
	values, err := readSelfHostEnv(filepath.Join(directory, ".env"))
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"CULLY_MCP_HOST", "CULLY_MCP_AUTH_CONNECTOR", "CULLY_JWKS_URL", "MCP_AUTH_UPSTREAM_CLIENT_SECRET", "CULLY_AUTH_HOST", "CULLY_AUTH_ISSUER", "CULLY_AUTH_RESOURCE"} {
		if value := os.Getenv(key); value != "" {
			values[key] = value
		}
	}
	customAuth := os.Getenv("CULLY_CUSTOM_AUTH") == "1"
	required := []string{"CULLY_MCP_HOST", "CULLY_AUTH_HOST"}
	if !customAuth {
		required = append(required, "MCP_AUTH_UPSTREAM_CLIENT_SECRET")
	}
	for _, key := range required {
		if !existingSelfHostSecret(values[key]) || strings.Contains(values[key], "example.com") {
			return nil, fmt.Errorf("set a real %s in .env", key)
		}
	}
	for _, key := range []string{"CULLY_MCP_HOST", "CULLY_AUTH_HOST"} {
		if strings.Contains(values[key], "/") || strings.Contains(values[key], "://") {
			return nil, fmt.Errorf("%s must be a hostname, not a URL", key)
		}
	}
	resource := "https://" + values["CULLY_MCP_HOST"] + "/mcp"
	if values["CULLY_AUTH_RESOURCE"] != "" && values["CULLY_AUTH_RESOURCE"] != resource {
		return nil, fmt.Errorf("CULLY_AUTH_RESOURCE must equal the public MCP URL derived from CULLY_MCP_HOST")
	}
	values["CULLY_AUTH_RESOURCE"] = resource
	if values["CULLY_AUTH_ISSUER"] == "" {
		values["CULLY_AUTH_ISSUER"] = "https://" + values["CULLY_AUTH_HOST"] + "/mcp-auth"
		if customAuth {
			values["CULLY_AUTH_ISSUER"] = "https://" + values["CULLY_AUTH_HOST"]
		}
	}
	if values["CULLY_JWKS_URL"] == "" {
		values["CULLY_JWKS_URL"] = strings.TrimRight(values["CULLY_AUTH_ISSUER"], "/") + "/.well-known/jwks.json"
	}
	for _, key := range []string{"CULLY_AUTH_ISSUER", "CULLY_AUTH_RESOURCE", "CULLY_JWKS_URL"} {
		if strings.Contains(values[key], "example.com") || strings.HasPrefix(values[key], "replace-with-") || !strings.HasPrefix(values[key], "https://") {
			return nil, fmt.Errorf("%s must be a real HTTPS URL", key)
		}
	}
	if customAuth {
		values["CULLY_MCP_AUTH_CONNECTOR"] = ""
		return values, nil // Provider-specific validation belongs to the chosen auth image.
	}
	contents, err := os.ReadFile(filepath.Join(directory, "connectors.json"))
	if err != nil {
		return nil, err
	}
	var connectors map[string]json.RawMessage
	if err := json.Unmarshal(contents, &connectors); err != nil || len(connectors) == 0 {
		return nil, fmt.Errorf("connectors.json must define at least one connector")
	}
	selected := values["CULLY_MCP_AUTH_CONNECTOR"]
	if selected == "" {
		if len(connectors) != 1 {
			return nil, fmt.Errorf("set CULLY_MCP_AUTH_CONNECTOR when connectors.json defines multiple connectors")
		}
		for key := range connectors {
			selected = key
		}
	}
	var connector map[string]json.RawMessage
	if raw, ok := connectors[selected]; !ok || json.Unmarshal(raw, &connector) != nil || connector == nil {
		return nil, fmt.Errorf("connectors.json must define selected connector %q", selected)
	}
	var secretEnv string
	if err := json.Unmarshal(connector["client_secret_env"], &secretEnv); err != nil || secretEnv != "MCP_AUTH_UPSTREAM_CLIENT_SECRET" {
		return nil, fmt.Errorf("selected connector must use client_secret_env MCP_AUTH_UPSTREAM_CLIENT_SECRET")
	}
	values["CULLY_MCP_AUTH_CONNECTOR"] = selected
	return values, nil
}
