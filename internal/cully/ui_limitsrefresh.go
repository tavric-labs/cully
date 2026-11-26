package cully

import (
	"bufio"
	"context"
	"net/http"
	"os/exec"
	"time"
)

// Refreshing the usage limits. Claude Code tells a running task about the account's limits as it
// works, so with nothing running the page only has what it heard last. A refresh asks each account
// one tiny question (a few tokens on the smallest model) and reads the limits from the answer's
// events. It runs only when you press the button.
const uiLimitsRefreshTimeout = 90 * time.Second

func (m *uiManager) refreshLimits() (int, error) {
	bin := m.cfg.ClaudeBin
	if bin == "" {
		bin = claudeExecutable()
	}
	n := m.cfg.Tokens.count()
	accounts := []int{0} // 0 is this machine's own Claude login
	if n > 0 {
		accounts = accounts[:0]
		for i := 1; i <= n; i++ {
			accounts = append(accounts, i)
		}
	}
	heard := 0
	for _, acct := range accounts {
		ctx, cancel := context.WithTimeout(context.Background(), uiLimitsRefreshTimeout)
		cmd := exec.CommandContext(ctx, bin, "-p", "Reply with OK.", "--max-turns", "1", "--model", "haiku", "--output-format", "stream-json", "--verbose", "--permission-mode", "plan", "--no-session-persistence", "--tools", "")
		cmd.Env = uiAgentEnv(m.cfg.Tokens.at(acct))
		cmd.Dir = ConfigDir()
		out, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err != nil {
			cancel()
			continue
		}
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			if p := parseStreamLine(sc.Bytes()); p.Limits != nil {
				p.Limits.Seen = time.Now().Unix()
				m.setLimits(acct, *p.Limits)
				heard++
			}
		}
		_ = cmd.Wait()
		cancel()
	}
	return heard, nil
}

func (s *uiServer) limitsRefresh(w http.ResponseWriter, r *http.Request) {
	n, err := s.m.refreshLimits()
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, map[string]int{"heard": n})
}
