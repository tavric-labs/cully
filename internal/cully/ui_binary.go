package cully

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// A harness updates itself by replacing its program file, and for a moment the link on your PATH can point at a
// version that was just removed. Starting a task in that moment fails with "no such file or directory", which says
// nothing useful. So before a task launches a harness, Cully waits briefly for the program to exist, looks it up
// again by name each time (an updater may have re-pointed the link), and, if it never appears, says what happened.

// Variables so a test need not wait.
var (
	uiBinaryWait = 20 * time.Second
	uiBinaryPoll = 500 * time.Millisecond
)

// uiAwaitBinary returns a program path that exists, waiting up to uiBinaryWait for one. It follows links, so a
// link whose target is missing counts as missing.
func uiAwaitBinary(bin string) (string, error) {
	name := filepath.Base(bin)
	deadline := time.Now().Add(uiBinaryWait)
	for {
		if st, err := os.Stat(bin); err == nil && !st.IsDir() {
			return bin, nil
		}
		// Look it up again by name, but only when it was found on PATH in the first place. A program the person
		// pointed at by an explicit path is never swapped for a different one that happens to be on PATH.
		if uiDirOnPath(filepath.Dir(bin)) {
			if p, err := exec.LookPath(name); err == nil {
				if st, err := os.Stat(p); err == nil && !st.IsDir() {
					return p, nil
				}
			}
		}
		if !time.Now().Before(deadline) {
			return "", fmt.Errorf("%s was missing, most likely because it was updating itself. Try Continue in a minute. (Cully looked for %s; run `%s --version` in a terminal to check it.)", name, bin, name)
		}
		time.Sleep(uiBinaryPoll)
	}
}

// uiDirOnPath reports whether dir is one of the folders on PATH.
func uiDirOnPath(dir string) bool {
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if d != "" && filepath.Clean(d) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}
