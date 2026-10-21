package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mcp-runtime/cully/internal/config"
	"github.com/mcp-runtime/cully/internal/identity"
	"github.com/mcp-runtime/cully/internal/mem0"
	"github.com/mcp-runtime/cully/internal/memory"
	"github.com/mcp-runtime/cully/internal/store/postgres"
	"github.com/mcp-runtime/cully/internal/store/remote"
	"github.com/mcp-runtime/cully/internal/transport/datahttp"
	mcpt "github.com/mcp-runtime/cully/internal/transport/mcp"
	"github.com/mcp-runtime/cully/migrations"
)

func lifetime() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
func serve(ctx context.Context, address string, handler http.Handler) error {
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	errorsCh := make(chan error, 1)
	go func() { errorsCh <- server.ListenAndServe() }()
	select {
	case err := <-errorsCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("HTTP listener failed")
	case <-ctx.Done():
	}
	stop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(stop)
}
func MCP(version string, oauthFlag bool) error {
	s, err := config.LoadMCP(oauthFlag)
	if err != nil {
		return err
	}
	store, err := remote.New(s.DataAPIURL, s.DataAPIToken)
	if err != nil {
		return err
	}
	ctx, cancel := lifetime()
	defer cancel()
	option := mcpt.AuthConfig{Mode: s.AuthMode, Owner: s.Owner, Issuer: s.Issuer, Resource: s.Resource}
	if s.AuthMode == "oauth" {
		option.Verifier, err = identity.NewVerifier(ctx, s.Issuer, s.Resource, s.JWKSURL)
		if err != nil {
			return fmt.Errorf("OAuth signing key initialization failed: %w", err)
		}
	}
	return serve(ctx, s.Address, mcpt.Handler(memory.Service{Store: store}, option, s.MCPPath, version))
}
func Data(command string) error {
	s, err := config.Load(true)
	if err != nil {
		return err
	}
	ctx, cancel := lifetime()
	defer cancel()
	cfg, err := pgxpool.ParseConfig(s.DatabaseURL)
	if err != nil {
		return fmt.Errorf("invalid CULLY_DATABASE_URL")
	}
	cfg.MaxConns = s.MaxConns
	cfg.MinConns = 1
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("database pool initialization failed")
	}
	defer pool.Close()
	if command == "migrate" {
		migrationCtx, stop := context.WithTimeout(ctx, 2*time.Minute)
		defer stop()
		if err := migrations.Apply(migrationCtx, pool); err != nil {
			return fmt.Errorf("database migration failed; check schema and ownership before retrying")
		}
		return nil
	}
	checkCtx, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	var schemaReady bool
	if err = pool.QueryRow(checkCtx, "SELECT EXISTS(SELECT 1 FROM cully_schema_versions WHERE version=$1)", migrations.RequiredVersion).Scan(&schemaReady); err != nil || !schemaReady {
		return fmt.Errorf("Cully schema is missing or behind (need version %d); run cully-data migrate", migrations.RequiredVersion)
	}
	store := &postgres.Store{Pool: pool}
	if s.Mem0URL != "" || s.Mem0APIKey != "" {
		store.Mem0, err = mem0.New(s.Mem0URL, s.Mem0APIKey)
		if err != nil {
			return err
		}
		store.Mem0Enabled = true
	}
	if command == "reindex" {
		return store.BackfillMem0(ctx)
	}
	if s.DataAPIToken == "" {
		return fmt.Errorf("CULLY_DATA_API_TOKEN is required")
	}
	var workers sync.WaitGroup
	workers.Add(1)
	go func() { defer workers.Done(); store.RunMem0Worker(ctx) }()
	slog.Info("Cully data API starting", "mem0_enabled", store.Mem0Enabled)
	err = serve(ctx, s.Address, datahttp.Handler(memory.Service{Store: store}, s.DataAPIToken, func(ctx context.Context) error {
		ctx, stop := context.WithTimeout(ctx, 2*time.Second)
		defer stop()
		return pool.Ping(ctx)
	}))
	cancel()
	workers.Wait()
	return err
}
func Health() error {
	port := os.Getenv("CULLY_PORT")
	if port == "" {
		port = "8083"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/readyz")
	if err != nil {
		return fmt.Errorf("not ready")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("not ready")
	}
	return nil
}
