//go:build windows

package cully

import (
	"fmt"
	"os"
)

func RunPane(_ string, _ []string, _, _ *os.File) error {
	return fmt.Errorf("the Cully terminal is not available on Windows")
}

func completedWindowSignals(_, _, _, fallbackSignals string) string { return fallbackSignals }
