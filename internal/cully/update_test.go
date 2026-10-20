package cully

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIUpdateVerifiesBeforeReplacingBinary(t *testing.T) {
	for _, failure := range []string{"", "checksum", "missing checksum", "checksum unavailable", "archive unavailable", "wrong version", "missing binary", "symlink", "truncated archive"} {
		t.Run(failure, func(t *testing.T) {
			var archive bytes.Buffer
			gz := gzip.NewWriter(&archive)
			tw := tar.NewWriter(gz)
			body := "#!/bin/sh\nprintf 'cully 1.2.3\\n'\n"
			if failure == "wrong version" {
				body = "#!/bin/sh\nprintf 'cully 0.0.1\\n'\n"
			}
			name, kind := "cully", byte(tar.TypeReg)
			if failure == "missing binary" {
				name = "../cully"
			}
			if failure == "symlink" {
				kind = tar.TypeSymlink
			}
			size := int64(len(body))
			if kind != tar.TypeReg {
				size = 0
			}
			if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: size, Typeflag: kind}); err != nil {
				t.Fatal(err)
			}
			if kind == tar.TypeReg {
				if _, err := tw.Write([]byte(body)); err != nil {
					t.Fatal(err)
				}
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			data := archive.Bytes()
			if failure == "truncated archive" {
				data = data[:len(data)/2]
			}
			sum := fmt.Sprintf("%x", sha256.Sum256(data))
			if failure == "checksum" {
				sum = strings.Repeat("0", 64)
			}
			asset := "cully_test.tar.gz"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1.2.3/checksums.txt":
					if failure == "checksum unavailable" {
						http.Error(w, "unavailable", 503)
						return
					}
					if failure != "missing checksum" {
						fmt.Fprintf(w, "%s  %s\n", sum, asset)
					}
				case "/v1.2.3/" + asset:
					if failure == "archive unavailable" {
						http.NotFound(w, r)
						return
					}
					_, _ = w.Write(data)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			dir := t.TempDir()
			destination := filepath.Join(dir, "cully")
			if err := os.WriteFile(destination, []byte("previous binary"), 0755); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(dir, "agent-cully")
			if err := os.Symlink(destination, link); err != nil {
				t.Fatal(err)
			}
			u := cliUpdater{client: server.Client(), releases: server.URL}
			err := u.install(context.Background(), destination, "v1.2.3", asset)
			got, readErr := os.ReadFile(link)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if failure == "" {
				if err != nil || string(got) != body {
					t.Fatalf("update = %q, %v", got, err)
				}
			} else if err == nil || string(got) != "previous binary" {
				t.Fatalf("failed update changed installation: %q, %v", got, err)
			}
			staged, _ := filepath.Glob(filepath.Join(dir, ".cully-update-*"))
			if len(staged) != 0 {
				t.Fatalf("staged files left behind: %v", staged)
			}
		})
	}
}

func TestUpdateHelpAndInvalidOptionsDoNotChangeInstallation(t *testing.T) {
	var out bytes.Buffer
	if err := RunUpdate(&out, "dev", []string{"--help"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "deployment procedure") || !strings.Contains(out.String(), "--check") {
		t.Fatal(out.String())
	}
	for _, args := range [][]string{{"--version", "../bad"}, {"--version", "", "extra"}, {"--remote"}, {"services"}} {
		if err := RunUpdate(&out, "dev", args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if err := RunUpdate(&out, "1.2.3", []string{"--version", "v1.2.3"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Already up to date") {
		t.Fatal(out.String())
	}
	if err := RunUpdate(&out, "1.2.2", []string{"--version", "v1.2.3", "--check"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No changes made") {
		t.Fatal(out.String())
	}
}

func TestUpdateDownloadBounds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "too much data") }))
	defer server.Close()
	u := cliUpdater{client: server.Client()}
	if _, err := u.get(context.Background(), server.URL, 3); err == nil {
		t.Fatal("accepted oversized response")
	}
}
