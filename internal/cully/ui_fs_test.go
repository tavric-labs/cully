package cully

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func uiBrowseTree(t *testing.T) string {
	t.Helper()
	root, _ := canonicalDir(t.TempDir())
	for _, d := range []string{"alpha", "beta", ".hidden", "gamma/inner"} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o644)
	exec.Command("git", "-C", filepath.Join(root, "alpha"), "init", "-q").Run()
	return root
}

func TestBrowseListsFoldersOnlyAndMarksRepositories(t *testing.T) {
	root := uiBrowseTree(t)
	v, err := uiBrowse(root, false)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	byName := map[string]uiFolderEntry{}
	for _, e := range v.Entries {
		names = append(names, e.Name)
		byName[e.Name] = e
	}
	if strings.Join(names, ",") != "alpha,beta,gamma" {
		t.Fatalf("entries = %v, want folders only, no files and no hidden ones", names)
	}
	if !byName["alpha"].IsGit || byName["beta"].IsGit {
		t.Fatalf("git flags wrong: %+v", v.Entries)
	}
	if byName["alpha"].Path != filepath.Join(root, "alpha") || v.Path != root || v.Parent != filepath.Dir(root) {
		t.Fatalf("paths wrong: %+v", v)
	}
	with, _ := uiBrowse(root, true)
	found := false
	for _, e := range with.Entries {
		found = found || e.Name == ".hidden"
	}
	if !found {
		t.Fatal("hidden folders not shown when asked for")
	}
}

func TestBrowseSaysWhenTheCurrentFolderIsARepository(t *testing.T) {
	root := uiBrowseTree(t)
	inside, _ := uiBrowse(filepath.Join(root, "alpha"), false)
	if !inside.IsGit {
		t.Fatal("a repository folder is not marked as one")
	}
	if plain, _ := uiBrowse(filepath.Join(root, "beta"), false); plain.IsGit {
		t.Fatal("a plain folder is marked as a repository")
	}
}

func TestBrowseRefusesBadAndHiddenPaths(t *testing.T) {
	root := uiBrowseTree(t)
	for _, bad := range []string{"", "relative/path", filepath.Join(root, "file.txt"), filepath.Join(root, "nope")} {
		if _, err := uiBrowse(bad, false); err == nil {
			t.Fatalf("browse(%q) accepted", bad)
		}
	}
}

func TestBrowseShowsTheWholeTreeAndWhetherItIsWritable(t *testing.T) {
	root := uiBrowseTree(t)
	v, err := uiBrowse(root, false)
	if err != nil || !v.Writable || len(v.Entries) == 0 || !v.Entries[0].Writable {
		t.Fatalf("view = %+v, err = %v", v, err)
	}
}

func TestBrowseStartsInTheHomeFolder(t *testing.T) {
	home, _ := canonicalDir(t.TempDir())
	t.Setenv("HOME", home)
	v, err := uiBrowse(uiHomeMarker, false)
	if err != nil || v.Path != home || v.Home != home {
		t.Fatalf("view = %+v, err = %v", v, err)
	}
}

func TestBrowseEndpoint(t *testing.T) {
	root := uiBrowseTree(t)
	srv, m, _ := uiTestServer(t, false)
	m.cfg.HideSessionsIn = []string{filepath.Join(root, "gamma")}
	if r := uiDo(t, "GET", srv.URL+"/api/fs?path="+root, nil, nil); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	var v uiBrowseView
	rr := uiDo(t, "GET", srv.URL+"/api/fs?path="+root, nil, uiAuthed)
	if rr.StatusCode != 200 {
		t.Fatalf("browse: %d", rr.StatusCode)
	}
	decodeJSON(t, rr, &v)
	found := false
	for _, e := range v.Entries {
		found = found || e.Name == "gamma"
	}
	if !found {
		t.Fatal("a folder hidden from Sessions is missing from the folder browser")
	}
	if r := uiDo(t, "GET", srv.URL+"/api/fs?path="+filepath.Join(root, "gamma"), nil, uiAuthed); r.StatusCode != 200 {
		t.Fatalf("folder hidden from Sessions cannot be opened: %d", r.StatusCode)
	}
	if r := uiDo(t, "GET", srv.URL+"/api/fs?path=relative", nil, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("relative path: %d", r.StatusCode)
	}
}

func TestAppOffersAFolderBrowserAndStartsAnywhere(t *testing.T) {
	js, _ := uiStaticFS.ReadFile("ui_static/app.js")
	for _, want := range []string{"/api/fs", "Choose a folder", "Start somewhere else"} {
		if !strings.Contains(string(js), want) {
			t.Fatalf("app.js lacks %q", want)
		}
	}
}

func decodeJSON(t *testing.T, r *http.Response, v any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}

func TestAddFolderCanSetUpGitInAPlainFolder(t *testing.T) {
	m, _, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	plain := t.TempDir()
	if _, err := m.addRepoInit(plain, false); err == nil {
		t.Fatal("a plain folder was accepted without asking for git")
	}
	info, err := m.addRepoInit(plain, true)
	if err != nil {
		t.Fatal(err)
	}
	if !uiIsRepoRoot(info.Path) {
		t.Fatalf("not a repository: %+v", info)
	}
	if _, err := m.start(uiStartRequest{Repo: info.Path, Prompt: "go"}); err != nil {
		t.Fatalf("agent could not start in the new repository: %v", err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, err := m.addRepoInit(home, true); err == nil {
		t.Fatal("git init in the home folder was allowed")
	}
}

func TestTypedPathsAreExpandedSoAnyFolderCanBeChosen(t *testing.T) {
	home, _ := canonicalDir(t.TempDir())
	t.Setenv("HOME", home)
	sub := filepath.Join(home, "work", "app")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{"~": home, "~/work/app": sub, "  " + sub + "/  ": sub, "/": "/"} {
		if got := uiExpandPath(in); got != want {
			t.Errorf("uiExpandPath(%q) = %q, want %q", in, got, want)
		}
	}
	for _, typed := range []string{"~/work/app", sub + "/", "  ~/work  "} {
		if _, err := uiBrowse(typed, false); err != nil {
			t.Errorf("browse(%q): %v", typed, err)
		}
	}
	m, _, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	if _, err := m.addRepoInit("~/work/app", true); err != nil {
		t.Fatalf("a folder typed with ~ was refused: %v", err)
	}
}
