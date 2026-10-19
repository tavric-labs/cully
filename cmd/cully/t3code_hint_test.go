package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestPrintT3CodeHint(t *testing.T) {
	var out bytes.Buffer
	printT3CodeHint(&out, func(string) (string, error) { return "", errors.New("not found") })
	if out.Len() != 0 {
		t.Fatalf("hint printed without t3 on PATH: %q", out.String())
	}

	printT3CodeHint(&out, func(name string) (string, error) {
		if name != "t3" {
			t.Fatalf("looked up %q, want t3", name)
		}
		return "/usr/bin/t3", nil
	})
	if !strings.Contains(out.String(), "T3 Code detected") {
		t.Fatalf("hint missing: %q", out.String())
	}
}
