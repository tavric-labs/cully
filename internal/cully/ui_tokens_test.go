package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTokenFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tokens.env")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUITokensRoundRobinNumericOrder(t *testing.T) {
	path := writeTokenFile(t, "CLAUDE_CODE_OAUTH_TOKEN_2=bbb\nCLAUDE_CODE_OAUTH_TOKEN_10=jjj\nCLAUDE_CODE_OAUTH_TOKEN_1='aaa'\n")
	pool, err := loadUITokens(path)
	if err != nil {
		t.Fatal(err)
	}
	if pool.count() != 3 {
		t.Fatalf("count = %d, want 3", pool.count())
	}
	want := []struct {
		idx int
		val string
	}{{1, "aaa"}, {2, "bbb"}, {3, "jjj"}, {1, "aaa"}}
	for i, w := range want {
		idx, val := pool.take()
		if idx != w.idx || val != w.val {
			t.Fatalf("take %d = (%d,%q), want (%d,%q)", i, idx, val, w.idx, w.val)
		}
	}
}

func TestUITokensMissingFileIsEmpty(t *testing.T) {
	pool, err := loadUITokens(filepath.Join(t.TempDir(), "absent.env"))
	if err != nil {
		t.Fatal(err)
	}
	if idx, val := pool.take(); idx != 0 || val != "" || pool.count() != 0 {
		t.Fatalf("empty pool returned (%d,%q) count %d", idx, val, pool.count())
	}
}

func TestUITokensRedact(t *testing.T) {
	pool, _ := loadUITokens(writeTokenFile(t, "CLAUDE_CODE_OAUTH_TOKEN_1=sekret-value-123\n"))
	got := pool.redact("header sekret-value-123 and again sekret-value-123")
	if strings.Contains(got, "sekret-value-123") || strings.Count(got, "[redacted]") != 2 {
		t.Fatalf("redact = %q", got)
	}
}

func TestUITokensIgnoresOtherKeysAndComments(t *testing.T) {
	pool, _ := loadUITokens(writeTokenFile(t, "# comment\nOTHER=zzz\nANTHROPIC_API_KEY=nope\nCLAUDE_CODE_OAUTH_TOKEN_1=aaa\nCLAUDE_CODE_OAUTH_TOKEN_X=bad\n"))
	if pool.count() != 1 {
		t.Fatalf("count = %d, want 1", pool.count())
	}
}
