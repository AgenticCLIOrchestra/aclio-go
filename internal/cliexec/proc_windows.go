//go:build windows

package cliexec

import (
	"os/exec"
	"syscall"
)

// detachedSysProcAttr gives the child its own process group so it isn't wired
// to the console's ctrl+c. Windows has no sessions in the unix sense; this is
// the closest equivalent to the Setsid detach used on unix.
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// interruptDetached does nothing on windows: there is no session-wide signal
// via negative pid, and Go cannot deliver a console ctrl+c to another process
// group cleanly. Returning false tells the guard no polite signal was sent, so
// it skips the grace window and hard-kills the direct child straight away (see
// killDetached's caveat) rather than idling for gracePeriod.
func interruptDetached(cmd *exec.Cmd) bool { return false }

// killDetached kills the child process. Windows has no process-group kill via
// negative pid; killing the direct child is the best effort here (grandchildren
// the CLI spawned may survive — documented limitation, not fixed here).
func killDetached(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
