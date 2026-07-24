//go:build unix

package cliexec

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// swapInterruptChan makes the guard listen on ch instead of real OS signals,
// so tests inject interrupts deterministically. Returns a restore func.
func swapInterruptChan(ch <-chan os.Signal) (restore func()) {
	orig := interruptChan
	interruptChan = func() (<-chan os.Signal, func()) { return ch, func() {} }
	return func() { interruptChan = orig }
}

// setGrace shortens the SIGINT→SIGKILL window for tests. Returns a restore func.
func setGrace(d time.Duration) (restore func()) {
	orig := gracePeriod
	gracePeriod = d
	return func() { gracePeriod = orig }
}

// waitForReady blocks until the child creates its "ready" marker in dir — proof
// it is running and has installed any signal trap — so the test injects the
// interrupt at a deterministic point rather than after a hopeful fixed sleep.
// It touches only the filesystem, never cmd, so it stays race-free against the
// goroutine running Capture.
func waitForReady(t *testing.T, dir string) {
	t.Helper()
	marker := filepath.Join(dir, "ready")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("child did not signal readiness (marker %q) in time", marker)
}

// TestCommandDetachesSession — the child runs in its own session (D3.1).
func TestCommandDetachesSession(t *testing.T) {
	cmd := Command(t.TempDir(), "sleep", []string{"30"})
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		killDetached(cmd)
		_ = cmd.Wait()
	}()

	childSid, err := syscall.Getsid(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	mySid, err := syscall.Getsid(0)
	if err != nil {
		t.Fatal(err)
	}
	if childSid == mySid {
		t.Errorf("child shares session %d with the test; want it detached", childSid)
	}
	if childSid != cmd.Process.Pid {
		t.Errorf("child session id %d != pid %d; child is not a session leader", childSid, cmd.Process.Pid)
	}
}

// TestGuardKillsProcessGroup — an interrupt reaps the child's whole group,
// through Capture, and Capture reports ErrInterrupted (D3.2, group side).
//
// The whole group ignores SIGINT — an ignored disposition is inherited by the
// backgrounded grandchild across fork+exec — so only the guard's escalation
// SIGKILL can reap it, which also gives cmd.Wait a clean happens-before edge
// (the group dies because of the guard, not out from under it). sh prints its
// own pid, which under Setsid is the session/group leader id.
func TestGuardKillsProcessGroup(t *testing.T) {
	defer setGrace(100 * time.Millisecond)()
	injected := make(chan os.Signal, 1)
	defer swapInterruptChan(injected)()

	dir := t.TempDir()
	// touch ready after the trap is set and the grandchild is backgrounded, so
	// readiness means "whole group up and ignoring SIGINT".
	cmd := Command(dir, "sh", []string{"-c", "trap '' INT; echo $$; sleep 60 & touch ready; sleep 60"})
	var out string
	var capErr error
	done := make(chan struct{})
	go func() {
		out, capErr = Capture(cmd)
		close(done)
	}()

	waitForReady(t, dir)
	injected <- os.Interrupt

	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("Capture did not return after interrupt")
	}

	if !errors.Is(capErr, ErrInterrupted) {
		t.Errorf("Capture err = %v, want ErrInterrupted", capErr)
	}
	pgid, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("could not parse group pid from child stdout %q: %v", out, err)
	}
	// SIGKILL reaped the leader and the backgrounded grandchild; group is gone.
	if e := syscall.Kill(-pgid, 0); !errors.Is(e, syscall.ESRCH) {
		t.Errorf("process group %d still present after interrupt (kill probe: %v)", pgid, e)
	}
}

// TestCaptureReturnsErrInterrupted — an interrupt makes Capture return
// ErrInterrupted rather than exiting the process (D3.2, API side; D1).
func TestCaptureReturnsErrInterrupted(t *testing.T) {
	defer setGrace(50 * time.Millisecond)()
	injected := make(chan os.Signal, 1)
	defer swapInterruptChan(injected)()

	dir := t.TempDir()
	cmd := Command(dir, "sh", []string{"-c", "touch ready; sleep 60"})
	done := make(chan error, 1)
	go func() {
		_, err := Capture(cmd)
		done <- err
	}()

	waitForReady(t, dir)
	injected <- os.Interrupt

	select {
	case err := <-done:
		if !errors.Is(err, ErrInterrupted) {
			t.Errorf("Capture err = %v, want ErrInterrupted", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Capture did not return after interrupt")
	}
}

// TestGuardScopedToRun — after stop(), the guard goroutine is gone and a late
// signal is neither consumed nor acted on (D3.3).
func TestGuardScopedToRun(t *testing.T) {
	injected := make(chan os.Signal, 1)
	defer swapInterruptChan(injected)()

	cmd := Command(t.TempDir(), "true", nil)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	g := newInterruptGuard(cmd)
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	g.stop() // deregisters and waits for the goroutine to exit

	// The goroutine has exited; this buffered send is never consumed, so no
	// kill is attempted and the flag stays false.
	injected <- os.Interrupt
	if g.wasInterrupted() {
		t.Error("guard fired after stop(); it must be scoped to the run")
	}
}

// TestGracefulInterruptNoSigkill — a child that handles SIGINT and exits within
// the grace window is not escalated to SIGKILL (D3.4, graceful path).
func TestGracefulInterruptNoSigkill(t *testing.T) {
	defer setGrace(1 * time.Second)()
	injected := make(chan os.Signal, 1)
	defer swapInterruptChan(injected)()

	dir := t.TempDir()
	cmd := Command(dir, "sh", []string{"-c", "trap 'exit 0' INT; touch ready; sleep 60"})
	done := make(chan error, 1)
	go func() {
		_, err := Capture(cmd)
		done <- err
	}()

	waitForReady(t, dir)
	injected <- os.Interrupt

	select {
	case err := <-done:
		if !errors.Is(err, ErrInterrupted) {
			t.Errorf("Capture err = %v, want ErrInterrupted", err)
		}
		// The child trapped SIGINT and exited 0, so the error must not be a
		// kill — proves the grace window worked and SIGKILL was skipped.
		if strings.Contains(err.Error(), "killed") {
			t.Errorf("child was SIGKILLed; graceful SIGINT should have sufficed: %v", err)
		}
	// Comfortably past the 1s grace: if the child failed to trap and was
	// SIGKILLed at ~1s, the "killed" assertion fires instead of a timeout.
	case <-time.After(5 * time.Second):
		t.Fatal("Capture did not return after a graceful interrupt")
	}
}

// TestStubbornChildSigkilled — a child that ignores SIGINT is SIGKILLed after
// the grace window (D3.4, escalation path).
func TestStubbornChildSigkilled(t *testing.T) {
	defer setGrace(150 * time.Millisecond)()
	injected := make(chan os.Signal, 1)
	defer swapInterruptChan(injected)()

	dir := t.TempDir()
	cmd := Command(dir, "sh", []string{"-c", "trap '' INT; touch ready; sleep 60"})
	done := make(chan error, 1)
	go func() {
		_, err := Capture(cmd)
		done <- err
	}()

	waitForReady(t, dir)
	injected <- os.Interrupt

	select {
	case err := <-done:
		if !errors.Is(err, ErrInterrupted) {
			t.Errorf("Capture err = %v, want ErrInterrupted", err)
		}
		if !strings.Contains(err.Error(), "killed") {
			t.Errorf("child ignored SIGINT and should have been SIGKILLed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Capture did not return after escalation")
	}
}
