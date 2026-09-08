//go:build !windows

package cli

import (
	"os/exec"
	"syscall"
)

// detachProcess puts the child in its own session so closing the terminal
// does not take the app down with it.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
