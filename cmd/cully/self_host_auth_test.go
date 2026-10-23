package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	selfhost "github.com/mcp-runtime/cully/deploy/self-hosted"
)

func TestMCPAuthInputOptions(t *testing.T) {
	inputs := []string{"--mcp-auth-image", "example/auth:v1", "--mcp-auth-config-dir", "/config", "--mcp-auth-env", "/provider.env"}
	o, err := parseSetup(append([]string{"--all", "--oauth"}, inputs...))
	if err != nil || o.auth.Image != "example/auth:v1" || o.auth.ConfigDir != "/config" || o.auth.EnvFile != "/provider.env" {
		t.Fatalf("options: %+v, %v", o, err)
	}
	production, err := parseSetup([]string{"--all", "--oauth", "--mcp-auth-config-dir", "/config", "--mcp-auth-env", "/provider.env"})
	if err != nil || production.auth.Image != "" {
		t.Fatalf("production unexpectedly requires an image: %+v %v", production, err)
	}
	for _, args := range [][]string{
		append([]string{"--all"}, inputs...),
		append([]string{"--mcp-url", "https://mcp.example/mcp", "--oauth"}, inputs...),
		{"--all", "--oauth", "--mcp-auth-config-dir", "/config"},
		{"--all", "--oauth", "--mcp-auth-image", "", "--mcp-auth-config-dir", "", "--mcp-auth-env", ""},
	} {
		if _, err := parseSetup(args); err == nil {
			t.Fatalf("accepted invalid auth options: %v", args)
		}
	}
}

func TestMCPAuthKeepsUserFilesAndPersistsOnlyImageAndPaths(t *testing.T) {
	old := latestMCPAuthImage
	lookups := 0
	latestMCPAuthImage = func() (string, error) { lookups++; return "registry.example/auth:released", nil }
	t.Cleanup(func() { latestMCPAuthImage = old })
	root := t.TempDir()
	config, setup, source := filepath.Join(root, "managed"), filepath.Join(root, "release"), filepath.Join(root, "my auth configs")
	for _, dir := range []string{config, setup, source} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	input := filepath.Join(source, "custom-provider-name.json")
	provider := filepath.Join(root, "provider.env")
	if err := os.WriteFile(input, []byte(`{"custom":"owned by mcp-auth"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(provider, []byte("CUSTOM_PROVIDER_SECRET=private-test-value\nMCP_AUTH_LOGIN_METHODS_FILE=/etc/mcp-auth/custom-provider-name.json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := mcpAuthOptions{Image: "registry.example/auth:v9", ConfigDir: source, EnvFile: provider}
	got, enabled, err := prepareMCPAuth(want, config, setup)
	if err != nil || !enabled || got != want {
		t.Fatalf("prepare: %+v %v %v", got, enabled, err)
	}
	saved, err := os.ReadFile(filepath.Join(config, "mcp-auth.json"))
	if err != nil || strings.Contains(string(saved), "private-test-value") || strings.Contains(string(saved), "owned by") {
		t.Fatalf("saved settings leaked config contents: %v", err)
	}
	got, enabled, err = prepareMCPAuth(mcpAuthOptions{}, config, setup)
	want.Image = "registry.example/auth:released"
	if err != nil || !enabled || got != want || lookups != 1 {
		t.Fatalf("resume: %+v %v %v", got, enabled, err)
	}
	want.Image = ""
	got, enabled, err = prepareMCPAuth(want, config, setup)
	if err != nil || !enabled || got.Image != "registry.example/auth:released" || lookups != 2 {
		t.Fatalf("production setup did not resolve latest: %+v %v %v", got, enabled, err)
	}
	if contents, _ := os.ReadFile(input); string(contents) != `{"custom":"owned by mcp-auth"}` {
		t.Fatal("modified user config")
	}
	if info, err := os.Stat(filepath.Join(config, "mcp-auth.json")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("saved settings are not private")
	}
	if data, _ := os.ReadFile(filepath.Join(setup, "compose.custom-auth.yaml")); string(data) != selfhost.CustomAuthCompose {
		t.Fatal("missing managed auth wiring")
	}
	if _, _, err := prepareMCPAuth(mcpAuthOptions{Image: "image:v1", ConfigDir: provider, EnvFile: source}, config, setup); err == nil {
		t.Fatal("accepted reversed file/directory inputs")
	}
}

func TestLatestMCPAuthReleaseResolution(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		image      string
	}{
		{"stable", `{"tag_name":"v0.9.2","draft":false,"prerelease":false}`, 200, "docker.io/princekrroshan01/mcp-auth-server:0.9.2"},
		{"untagged-prefix", `{"tag_name":"0.9.2"}`, 200, "docker.io/princekrroshan01/mcp-auth-server:0.9.2"},
		{"draft", `{"tag_name":"v0.9.2","draft":true}`, 200, ""},
		{"prerelease", `{"tag_name":"v1.0.0-rc1","prerelease":true}`, 200, ""},
		{"invalid tag", `{"tag_name":"latest"}`, 200, ""},
		{"invalid JSON", `broken`, 200, ""},
		{"rate limited", `{"message":"retry"}`, 403, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			image, err := fetchLatestMCPAuthImage(context.Background(), server.Client(), server.URL)
			if image != tc.image || (err == nil) != (tc.image != "") {
				t.Fatalf("release: %q %v", image, err)
			}
		})
	}
}

func TestCustomAuthDoesNotRequireLegacyConnectorOrSecret(t *testing.T) {
	for _, key := range []string{"CULLY_MCP_HOST", "CULLY_AUTH_HOST", "CULLY_AUTH_ISSUER", "CULLY_AUTH_RESOURCE", "CULLY_JWKS_URL", "CULLY_MCP_AUTH_CONNECTOR", "MCP_AUTH_UPSTREAM_CLIENT_SECRET"} {
		t.Setenv(key, "")
	}
	t.Setenv("CULLY_CUSTOM_AUTH", "1")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("CULLY_MCP_HOST=mcp.acme.test\nCULLY_AUTH_HOST=auth.acme.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	values, err := selfHostOAuthValues(root)
	if err != nil || values["CULLY_AUTH_ISSUER"] != "https://auth.acme.test" || values["CULLY_AUTH_RESOURCE"] != "https://mcp.acme.test/mcp" || values["CULLY_MCP_AUTH_CONNECTOR"] != "" {
		t.Fatalf("custom auth trust: %+v, %v", values, err)
	}
	t.Setenv("CULLY_CUSTOM_AUTH", "")
	if _, err := selfHostOAuthValues(root); err == nil {
		t.Fatal("legacy mode no longer requires its connector secret")
	}
}

func TestCustomAuthSetupScriptSelectsUserService(t *testing.T) {
	root := t.TempDir()
	write := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, ".secrets"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(".secrets/signing-key.pem", "test-key")
	write(".env", "# test\n")
	write("setup.sh", selfhost.SetupScript)
	write("docker", "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$TEST_DOCKER_LOG\"\n")
	write("cully", `#!/bin/sh
case "$*" in
  "_internal self-hosted-credentials oauth-env mcp-auth")
    echo 'export CULLY_AUTH_RESOURCE=https://mcp.acme.test/mcp'
    echo 'export CULLY_AUTH_ISSUER=https://auth.acme.test'
    echo 'export CULLY_JWKS_URL=https://auth.acme.test/.well-known/jwks.json' ;;
esac
`)
	command := exec.Command("sh", filepath.Join(root, "setup.sh"), "--oauth", "mcp-auth", "codex")
	command.Dir = root
	command.Env = append(os.Environ(), "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"), "CULLY_CLI_BINARY="+filepath.Join(root, "cully"), "CULLY_CUSTOM_AUTH=1", "TEST_DOCKER_LOG="+filepath.Join(root, "docker.log"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("setup script: %v: %s", err, output)
	}
	calls, err := os.ReadFile(filepath.Join(root, "docker.log"))
	if err != nil || !strings.Contains(string(calls), "-f compose.custom-auth.yaml --profile oauth") || !strings.Contains(string(calls), "mcp mcp-auth-custom caddy") || strings.Contains(string(calls), "--profile mcp-auth up") {
		t.Fatalf("wrong auth service selected: %s, %v", calls, err)
	}
}
