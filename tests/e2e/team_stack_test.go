package e2e

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mcp-runtime/cully/internal/memory"
	"github.com/mcp-runtime/cully/internal/workspace"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type teamStack struct {
	ctx                   context.Context
	endpoint, issuer, cli string
	key                   *rsa.PrivateKey
	pool                  *pgxpool.Pool
	env                   []string
}

// startTeamStack runs the production executables, migrations and JWKS verifier.
// Only the identity provider is a fixture; no memory or workspace handler is mocked.
func startTeamStack(t *testing.T) *teamStack {
	t.Helper()
	database := os.Getenv("CULLY_E2E_TEAM_DATABASE_URL")
	if database == "" {
		t.Skip("CULLY_E2E_TEAM_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	admin, err := pgxpool.New(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	schema := "team_e2e_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	u, err := url.Parse(database)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "team-e2e", "alg": "RS256", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB",
		}}})
	}))
	t.Cleanup(jwks.Close)
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	for _, name := range []string{"cully-data", "cully-mcp", "cully"} {
		cmd := exec.CommandContext(ctx, "go", "build", "-race", "-o", filepath.Join(bin, name), "./cmd/"+name)
		cmd.Dir = root
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", name, err, output)
		}
	}
	// Ignore the developer's service configuration and credentials in child processes.
	env := []string{}
	for _, entry := range os.Environ() {
		name := strings.SplitN(entry, "=", 2)[0]
		if !strings.HasPrefix(name, "CULLY_") && !strings.HasPrefix(name, "MCP_AUTH_") && name != "MCP_PATH" {
			env = append(env, entry)
		}
	}
	dataPort, mcpPort := unusedPort(t), unusedPort(t)
	dataURL, endpoint := "http://127.0.0.1:"+dataPort, "http://127.0.0.1:"+mcpPort+"/mcp"
	env = append(env, "CULLY_HOST=127.0.0.1", "CULLY_DATABASE_URL="+u.String(), "CULLY_DATA_API_TOKEN="+uuid.NewString(), "CULLY_DATA_API_URL="+dataURL)
	migrate := exec.CommandContext(ctx, filepath.Join(bin, "cully-data"), "migrate")
	migrate.Env = env
	if output, err := migrate.CombinedOutput(); err != nil {
		t.Fatalf("migration: %v\n%s", err, output)
	}
	startProcess(t, ctx, filepath.Join(bin, "cully-data"), append(append([]string{}, env...), "CULLY_PORT="+dataPort), dataURL+"/readyz")
	mcpEnv := append(append([]string{}, env...), "CULLY_PORT="+mcpPort, "CULLY_MCP_AUTH_MODE=oauth", "CULLY_AUTH_ISSUER="+jwks.URL, "CULLY_AUTH_RESOURCE="+endpoint, "CULLY_JWKS_URL="+jwks.URL)
	startProcess(t, ctx, filepath.Join(bin, "cully-mcp"), mcpEnv, "http://127.0.0.1:"+mcpPort+"/healthz")
	return &teamStack{ctx: ctx, endpoint: endpoint, issuer: jwks.URL, key: key, pool: pool, cli: filepath.Join(bin, "cully"), env: env}
}

func unusedPort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func startProcess(t *testing.T, ctx context.Context, binary string, env []string, readyURL string) {
	t.Helper()
	log, err := os.CreateTemp(t.TempDir(), "service-*.log")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Close() })
	cmd := exec.CommandContext(ctx, binary)
	cmd.Env, cmd.Stdout, cmd.Stderr = env, log, log
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})
	client := &http.Client{Timeout: time.Second}
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	for {
		resp, err := client.Get(readyURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case err := <-done:
			// Keep cleanup from waiting on an already consumed result.
			done <- err
			output, _ := os.ReadFile(log.Name())
			t.Fatalf("%s exited: %v\n%s", filepath.Base(binary), err, output)
		case <-deadline.C:
			t.Fatalf("%s never became ready", filepath.Base(binary))
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (s *teamStack) token(t *testing.T, subject, scope, issuer, audience string, expiration time.Time) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"iss": issuer, "aud": audience, "sub": subject, "exp": expiration.Unix(), "scope": scope})
	token.Header["kid"] = "team-e2e"
	raw, err := token.SignedString(s.key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func (s *teamStack) connect(t *testing.T, subject, scope string) *sdk.ClientSession {
	t.Helper()
	raw := s.token(t, subject, scope, s.issuer, s.endpoint, time.Now().Add(time.Hour))
	client := sdk.NewClient(&sdk.Implementation{Name: "team-e2e", Version: "1"}, nil)
	session, err := client.Connect(s.ctx, &sdk.StreamableClientTransport{Endpoint: s.endpoint, HTTPClient: &http.Client{Transport: bearerTransport{raw}, Timeout: 10 * time.Second}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func callTool(t *testing.T, ctx context.Context, client *sdk.ClientSession, name string, input any) memory.Result {
	t.Helper()
	out, err := client.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: input})
	if err != nil || out == nil || out.IsError {
		content, _ := json.Marshal(out)
		t.Fatalf("%s failed: %v %s", name, err, content)
	}
	b, err := json.Marshal(out.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var result memory.Result
	if err = json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func callWorkspace(t *testing.T, s *teamStack, client *sdk.ClientSession, input workspace.Input) *workspace.Result {
	t.Helper()
	name := "cully_workspace_read"
	if input.Write() {
		name = "cully_workspace_write"
	}
	result := callTool(t, s.ctx, client, name, input).Workspace
	if result == nil {
		t.Fatal("workspace result missing")
	}
	return result
}

func rejectWorkspace(t *testing.T, s *teamStack, client *sdk.ClientSession, input workspace.Input, reason string) {
	t.Helper()
	name := "cully_workspace_read"
	if input.Write() {
		name = "cully_workspace_write"
	}
	result, err := client.CallTool(s.ctx, &sdk.CallToolParams{Name: name, Arguments: input})
	if err != nil || result == nil || !result.IsError {
		t.Fatalf("%s: expected rejection, got %v %+v", reason, err, result)
	}
}
