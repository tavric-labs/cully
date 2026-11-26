package cully

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const uiUsage = "usage: cully ui [--addr HOST:PORT] [--repo DIR]... [--agent claude|codex|cursor]... [--tokens FILE] [--max-agents N] [--tls-cert FILE --tls-key FILE] [--quiet]\n       cully ui url"

type uiOptions struct {
	Addr      string
	Repos     []string
	Tokens    string
	MaxAgents int
	TLSCert   string
	TLSKey    string
	Quiet     bool

	NoConflictAlerts bool
	Agents           []string // agents you launch Cully with; the UI focuses on these
	HideSessionsIn   []string // folders whose sessions are never listed
}

type uiAgentFlag []string

func (a *uiAgentFlag) String() string     { return strings.Join(*a, ",") }
func (a *uiAgentFlag) Set(v string) error { *a = append(*a, v); return nil }

type uiRepoFlag []string

func (r *uiRepoFlag) String() string     { return strings.Join(*r, ",") }
func (r *uiRepoFlag) Set(v string) error { *r = append(*r, v); return nil }

func parseUIFlags(args []string) (uiOptions, error) {
	fs := flag.NewFlagSet("ui", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var repos uiRepoFlag
	var agents uiAgentFlag
	var hide uiRepoFlag
	o := uiOptions{}
	fs.StringVar(&o.Addr, "addr", "127.0.0.1:7777", "listen address")
	fs.Var(&repos, "repo", "git repository agents may work in (repeatable; default: current directory)")
	fs.StringVar(&o.Tokens, "tokens", defaultUITokensFile(), "file with CLAUDE_CODE_OAUTH_TOKEN_N entries")
	fs.IntVar(&o.MaxAgents, "max-agents", 4, "agents running at once")
	fs.StringVar(&o.TLSCert, "tls-cert", "", "TLS certificate file")
	fs.StringVar(&o.TLSKey, "tls-key", "", "TLS key file")
	fs.Var(&hide, "hide-sessions-in", "a folder whose sessions are never listed (repeatable; also CULLY_UI_HIDE_SESSIONS_IN, colon separated)")
	fs.Var(&agents, "agent", "an agent you launch Cully with: claude, codex or cursor (repeatable; the UI focuses on these)")
	fs.BoolVar(&o.NoConflictAlerts, "no-conflict-alerts", false, "do not warn agents when two of them edit the same file")
	fs.BoolVar(&o.Quiet, "quiet", false, "do not print the sign-in link (another command shows it)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return o, errors.New(uiUsage)
	}
	if (o.TLSCert == "") != (o.TLSKey == "") {
		return o, errors.New("--tls-cert and --tls-key go together")
	}
	if o.MaxAgents < 1 {
		return o, errors.New("--max-agents must be at least 1")
	}
	for _, a := range agents {
		if !uiKnownAgent(a) {
			return o, errors.New("--agent must be claude, codex or cursor")
		}
	}
	o.Agents = agents
	for _, h := range append([]string(hide), strings.FieldsFunc(os.Getenv("CULLY_UI_HIDE_SESSIONS_IN"), func(r rune) bool { return r == ':' })...) {
		if !filepath.IsAbs(h) {
			return o, errors.New("--hide-sessions-in needs a full path")
		}
		o.HideSessionsIn = append(o.HideSessionsIn, filepath.Clean(h))
	}
	o.Repos = repos
	if len(o.Repos) == 0 {
		o.Repos = []string{currentDir()}
	}
	return o, nil
}

func defaultUITokensFile() string {
	if p := os.Getenv("CULLY_UI_TOKENS_FILE"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cully", "tokens.env")
}

// uiLoopbackAddr reports whether a listen address is reachable only from this
// machine. An empty host means every interface.
func uiLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func uiExposureWarning(o uiOptions) string {
	if uiLoopbackAddr(o.Addr) || o.TLSCert != "" {
		return ""
	}
	return "warning: listening on " + o.Addr + " over plain HTTP. The login token and agent output cross the network unencrypted. " +
		"Prefer --addr 127.0.0.1:7777 with an SSH tunnel (ssh -L 7777:127.0.0.1:7777 HOST), or pass --tls-cert and --tls-key."
}

// loadOrCreateUIToken returns the UI login token, creating it (mode 0600) on
// first use so the same URL keeps working across restarts.
func loadOrCreateUIToken() (string, error) {
	path := filepath.Join(cullyDir(), "ui-token")
	if b, err := os.ReadFile(path); err == nil {
		if tok := strings.TrimSpace(string(b)); len(tok) >= 32 {
			_ = os.Chmod(path, 0o600)
			return tok, nil
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(raw)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(tok + "\n"); err != nil {
		return "", err
	}
	return tok, nil
}

// RunUI implements `cully ui`: the browser UI for spawning and talking to agents.
func RunUI(w io.Writer, args []string) error {
	if len(args) > 0 && args[0] == "url" {
		if len(args) != 1 {
			return errors.New(uiUsage)
		}
		link, _, err := EnsureUI(currentDir())
		if err != nil {
			return err
		}
		fmt.Fprintln(w, link)
		return nil
	}
	o, err := parseUIFlags(args)
	if err != nil {
		return err
	}
	tokens, err := loadUITokens(o.Tokens)
	if err != nil {
		return fmt.Errorf("read tokens file: %w", err)
	}
	token, err := loadOrCreateUIToken()
	if err != nil {
		return fmt.Errorf("create UI token: %w", err)
	}
	m := newUIManager(uiConfig{Repos: o.Repos, Tokens: tokens, MaxAgents: o.MaxAgents, NoConflictAlerts: o.NoConflictAlerts, ScoutStream: uiStreamScout, HideSessionsIn: o.HideSessionsIn})
	for _, a := range o.Agents {
		_ = m.mcp.markInUse(a)
	}
	ln, err := net.Listen("tcp", o.Addr)
	if err != nil {
		return err
	}
	scheme := "http"
	if o.TLSCert != "" {
		scheme = "https"
	}
	host := ln.Addr().String()
	if h, port, err := net.SplitHostPort(host); err == nil && (h == "::" || h == "0.0.0.0") {
		name, _ := os.Hostname()
		host = net.JoinHostPort(name, port)
	}
	if !o.Quiet {
		fmt.Fprintf(w, "Cully UI  %s://%s/?token=%s\n", scheme, host, token)
	}
	if repos := m.repoList(); len(repos) > 0 {
		fmt.Fprintf(w, "folders: %s\n", strings.Join(repoPaths(repos), ", "))
	} else {
		fmt.Fprintln(w, "folders: none yet; add a git repository folder from the page")
	}
	if tokens.count() == 0 {
		fmt.Fprintf(w, "tokens: none found in %s; agents use the Claude login of this user\n", o.Tokens)
	} else {
		fmt.Fprintf(w, "tokens: %d, assigned round-robin\n", tokens.count())
	}
	if warn := uiExposureWarning(o); warn != "" {
		fmt.Fprintln(w, warn)
	}
	srv := &http.Server{Handler: newUIServer(m, token, uiLoopbackAddr(o.Addr)), ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go m.runIndexer(ctx)
	go func() {
		<-ctx.Done()
		m.stopAll()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if srv.Shutdown(shutdown) != nil {
			_ = srv.Close()
		}
	}()
	if o.TLSCert != "" {
		err = srv.ServeTLS(ln, o.TLSCert, o.TLSKey)
	} else {
		err = srv.Serve(ln)
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
