package cully

import (
	"bufio"
	"errors"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var uiTokenKey = regexp.MustCompile(`^CLAUDE_CODE_OAUTH_TOKEN_(\d+)$`)

// uiTokenPool hands out Claude OAuth tokens round-robin. Values stay inside
// this type and the child environment of an agent; callers outside the process
// only ever see a 1-based index.
type uiTokenPool struct {
	mu   sync.Mutex
	vals []string
	next int
}

// loadUITokens reads CLAUDE_CODE_OAUTH_TOKEN_<N> entries from an env-style
// file. A missing file yields an empty pool, so agents fall back to the
// ambient Claude login.
func loadUITokens(path string) (*uiTokenPool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return &uiTokenPool{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	type entry struct {
		n   int
		val string
	}
	var entries []entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		m := uiTokenKey.FindStringSubmatch(strings.TrimSpace(key))
		if m == nil {
			continue
		}
		val = strings.Trim(strings.TrimSpace(val), `'"`)
		if val == "" {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		entries = append(entries, entry{n, val})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].n < entries[j].n })
	pool := &uiTokenPool{}
	for _, e := range entries {
		pool.vals = append(pool.vals, e.val)
	}
	return pool, nil
}

func (p *uiTokenPool) count() int {
	if p == nil {
		return 0
	}
	return len(p.vals)
}

// take returns the next token and its 1-based index; (0, "") when empty.
func (p *uiTokenPool) take() (int, string) {
	if p.count() == 0 {
		return 0, ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	i := p.next % len(p.vals)
	p.next = (p.next + 1) % len(p.vals)
	return i + 1, p.vals[i]
}

// at returns the token with the given 1-based index, or "".
func (p *uiTokenPool) at(i int) string {
	if p == nil || i < 1 || i > len(p.vals) {
		return ""
	}
	return p.vals[i-1]
}

// redact removes every token value from s.
func (p *uiTokenPool) redact(s string) string {
	if p.count() == 0 {
		return s
	}
	for _, v := range p.vals {
		s = strings.ReplaceAll(s, v, "[redacted]")
	}
	return s
}
