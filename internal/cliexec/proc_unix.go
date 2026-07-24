//go:build unix

package cliexec

import (
	"os/exec"
	"syscall"
)

// detachedSysProcAttr detaches the child into its own session so the driven
// CLI has no controlling terminal to manipulate (see Command).
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// interruptDetached sends a polite SIGINT to the child's whole session
// (negative pid targets the process group Setsid created), giving the child a
// chance to clean up before killDetached escalates. Delivered via kill(2), so
// it reaches the child regardless of the ISIG state of any terminal. Returns
// true when a signal was delivered, so the guard knows a grace window is
// warranted.
func interruptDetached(cmd *exec.Cmd) bool {
	if cmd.Process == nil {
		return false
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
	return true
}

// killDetached hard-kills the child's whole session (negative pid targets the
// process group Setsid created).
func killDetached(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
