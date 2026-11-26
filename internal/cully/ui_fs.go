package cully

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// A folder browser, so a new agent can start in any folder on the machine
// without typing its path. It lists folders only (never files), skips hidden
// ones unless asked. It shows the whole tree: hiding folders only applies to
// the Sessions list.

const (
	uiHomeMarker = "~"
	uiBrowseMax  = 500
)

type uiFolderEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsGit bool   `json:"isGit"`
	// Writable is whether this user can write in the folder, which agents need.
	Writable bool `json:"writable"`
}

type uiBrowseView struct {
	Path      string          `json:"path"`
	Parent    string          `json:"parent,omitempty"`
	Home      string          `json:"home"`
	IsGit     bool            `json:"isGit"` // this folder is a git repository root
	Writable  bool            `json:"writable"`
	Entries   []uiFolderEntry `json:"entries"`
	Truncated bool            `json:"truncated,omitempty"`
}

func uiIsRepoRoot(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func uiWritable(dir string) bool { return syscall.Access(dir, 2) == nil }

func uiBrowse(path string, showDot bool) (uiBrowseView, error) {
	home, _ := os.UserHomeDir()
	home, _ = canonicalDir(home)
	if path == uiHomeMarker {
		path = home
	}
	if !filepath.IsAbs(path) {
		return uiBrowseView{}, errors.New("give the full path of the folder")
	}
	real, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return uiBrowseView{}, errors.New("that folder does not exist")
	}
	info, err := os.Stat(real)
	if err != nil || !info.IsDir() {
		return uiBrowseView{}, errors.New("that is not a folder")
	}
	entries, err := os.ReadDir(real)
	if err != nil {
		return uiBrowseView{}, errors.New("that folder cannot be read")
	}
	v := uiBrowseView{Path: real, Home: home, IsGit: uiIsRepoRoot(real), Writable: uiWritable(real), Entries: []uiFolderEntry{}}
	if real != "/" {
		v.Parent = filepath.Dir(real)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && !showDot {
			continue
		}
		p := filepath.Join(real, name)
		isDir := e.IsDir()
		if !isDir && e.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				isDir = true
			}
		}
		if !isDir {
			continue
		}
		v.Entries = append(v.Entries, uiFolderEntry{Name: name, Path: p, IsGit: uiIsRepoRoot(p), Writable: uiWritable(p)})
	}
	sort.Slice(v.Entries, func(i, j int) bool { return strings.ToLower(v.Entries[i].Name) < strings.ToLower(v.Entries[j].Name) })
	if len(v.Entries) > uiBrowseMax {
		v.Entries, v.Truncated = v.Entries[:uiBrowseMax], true
	}
	return v, nil
}
