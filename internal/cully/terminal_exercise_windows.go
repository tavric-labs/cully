//go:build windows

package cully

import (
	"fmt"
	"os"
)

func RunTerminalExercise(_, _ *os.File) error {
	return fmt.Errorf("the Cully terminal is not available on Windows; host settings can be configured with cully terminal configure")
}
func RunTerminalCheckOutput(_, _ *os.File) error {
	return fmt.Errorf("the Cully terminal is not available on Windows")
}
