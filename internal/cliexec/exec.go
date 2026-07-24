package cliexec

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

// ErrInterrupted is returned by Capture/Stream when SIGINT/SIGTERM arrived
// while the child ran: the guard reaped the child (see the interrupt guard
// below) and the call returns this sentinel wrapping the child's wait error,
// rather than exiting the host process. Branch on it with errors.Is; a CLI
// front-end typically does `if errors.Is(err, cliexec.ErrInterrupted) {
// os.Exit(130) }` at its top level — the exit decision belongs to the binary,
// not the library.
var ErrInterrupted = errors.New("interrupted by signal")

// gracePeriod is how long the guard waits after the polite SIGINT before
// escalating to SIGKILL. Effectively constant — not an API knob (tunable only
// in-package, for tests).
var gracePeriod = 3 * time.Second

// interruptChan yields the channel the guard listens on plus a deregister func.
// A package var so tests can inject signals deterministically without touching
// the real process (see the test seam).
var interruptChan = defaultInterruptChan

func defaultInterruptChan() (<-chan os.Signal, func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	return ch, func() { signal.Stop(ch) }
}

// Command builds an invocation of bin in dir, detached into its own session:
// agentic CLIs manipulate the terminal they control even in print mode —
// notably disabling ISIG, which "eats" ctrl+c for the whole process group
// while they run. Detached, the child has no controlling terminal to break.
// The flip side is that terminal signals no longer reach it, so callers pair
// this with Capture/Stream, which install the interrupt guard.
func Command(dir, bin string, args []string) *exec.Cmd {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = detachedSysProcAttr()
	return cmd
}

// interruptGuard makes ctrl+c work while a detached child runs. Detachment
// orphaned the child from terminal signals, so on SIGINT/SIGTERM the guard
// reaps the child's whole session/group itself: a polite SIGINT, a grace
// window (cut short by a second signal, or by the child exiting on its own),
// then a hard SIGKILL. It records that an interrupt happened and returns — it
// does NOT exit the host process; the in-flight Capture/Stream call surfaces
// ErrInterrupted and the caller decides what to do. The guard is scoped to one
// run: created after Start, torn down by stop() when Wait returns.
type interruptGuard struct {
	cmd         *exec.Cmd
	sigs        <-chan os.Signal
	stopNotify  func()
	done        chan struct{} // closed by stop() once the run is over
	stopped     chan struct{} // closed when the guard goroutine exits
	interrupted atomic.Bool
}

func newInterruptGuard(cmd *exec.Cmd) *interruptGuard {
	sigs, stopNotify := interruptChan()
	g := &interruptGuard{
		cmd:        cmd,
		sigs:       sigs,
		stopNotify: stopNotify,
		done:       make(chan struct{}),
		stopped:    make(chan struct{}),
	}
	go g.run()
	return g
}

func (g *interruptGuard) run() {
	defer close(g.stopped)
	select {
	case <-g.sigs:
		g.reap()
	case <-g.done:
	}
}

// reap terminates the child's session/group: SIGINT first, then SIGKILL after
// the grace window. The window ends early on a second signal (the user is
// insisting — skip straight to the hard kill) or when the child exits on its
// own (done closed by stop() after Wait returns — nothing left to kill).
func (g *interruptGuard) reap() {
	g.interrupted.Store(true)

	if !interruptDetached(g.cmd) {
		// No polite signal was possible (e.g. windows) — there is nothing to
		// wait for, so skip the grace window and hard-kill immediately rather
		// than making the user's ctrl+c sit idle for gracePeriod.
		killDetached(g.cmd)
		return
	}

	select {
	case <-g.sigs:
		// second interrupt — escalate now
	case <-g.done:
		return // child already exited within the grace window
	case <-time.After(gracePeriod):
	}

	killDetached(g.cmd) // hard: SIGKILL the whole group
}

func (g *interruptGuard) stop() {
	g.stopNotify()
	close(g.done)
	<-g.stopped
}

func (g *interruptGuard) wasInterrupted() bool { return g.interrupted.Load() }

// interruptedErr builds the error returned when a run was interrupted. It wraps
// the child's wait error when there is one (typically "signal: killed"), and is
// the bare sentinel when the child happened to exit cleanly as the signal
// landed — so the message never reads "interrupted by signal: <nil>".
func interruptedErr(waitErr error) error {
	if waitErr != nil {
		return fmt.Errorf("%w: %v", ErrInterrupted, waitErr)
	}
	return ErrInterrupted
}

// Capture runs cmd to completion and returns its stdout, with the interrupt
// guard installed for the duration. On interrupt it returns ErrInterrupted
// (wrapping the child's wait error) rather than exiting the process.
func Capture(cmd *exec.Cmd) (string, error) {
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Start(); err != nil {
		return "", err
	}
	g := newInterruptGuard(cmd)
	defer g.stop()

	waitErr := cmd.Wait()
	if g.wasInterrupted() {
		return out.String(), interruptedErr(waitErr)
	}
	return out.String(), waitErr
}

// Stream runs cmd and calls onLine for each non-empty stdout line as it
// arrives, with the interrupt guard installed. It returns after the process
// exits. On interrupt it returns ErrInterrupted (wrapping the child's wait
// error) rather than exiting the process. The scanner buffer is sized
// generously for large single-line JSON events.
func Stream(cmd *exec.Cmd, onLine func(line string)) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting %s: %w", cmd.Path, err)
	}
	g := newInterruptGuard(cmd)
	defer g.stop()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			onLine(line)
		}
	}
	scanErr := scanner.Err()
	waitErr := cmd.Wait()

	if g.wasInterrupted() {
		return interruptedErr(waitErr)
	}
	if scanErr != nil {
		return fmt.Errorf("reading stream: %w", scanErr)
	}
	return waitErr
}
