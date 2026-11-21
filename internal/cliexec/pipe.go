package cliexec

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Pipe is a long-lived child process spoken to line by line: JSON lines go in
// on its stdin, and every non-empty stdout line comes out on Lines. Unlike
// Capture and Stream, nothing here waits for the child to exit, so one child
// can serve many requests. The child is built by Command, so it runs detached
// in its own session.
//
// Pipe installs no interrupt guard of its own: the caller decides how a
// signal maps onto the conversation (see Notify, Interrupt and Kill).
type Pipe struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser

	wmu sync.Mutex // serializes writes, so concurrent lines never interleave

	lines  chan string   // stdout lines, closed once the child has exited
	exited chan struct{} // closed once the child has exited and been waited for

	waitErr error // the child's wait error; read only after exited is closed
	readErr error // a stdout read error other than EOF; same
}

// Spawn starts cmd with its stdin and stdout piped. A goroutine reads stdout
// line by line (no size cap, like Stream) onto Lines, then waits for the
// child and closes Lines and Exited.
func Spawn(cmd *exec.Cmd) (*Pipe, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", cmd.Path, err)
	}

	p := &Pipe{
		cmd:    cmd,
		stdin:  stdin,
		lines:  make(chan string, 64),
		exited: make(chan struct{}),
	}
	go readPipe(p, stdout)
	return p, nil
}

func readPipe(p *Pipe, stdout io.Reader) {
	reader := bufio.NewReaderSize(stdout, 1024*1024)
	for {
		line, err := reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if line != "" {
			p.lines <- line
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			p.readErr = err
			break
		}
	}
	p.waitErr = p.cmd.Wait()
	close(p.lines)
	close(p.exited)
}

// Lines yields the child's stdout lines in order. It is closed once the child
// has exited and every line has been delivered.
func Lines(p *Pipe) <-chan string { return p.lines }

// Exited is closed once the child has exited and been waited for.
func Exited(p *Pipe) <-chan struct{} { return p.exited }

// ExitErr reports why the child ended: a stdout read error if there was one,
// otherwise its wait error (nil for a clean exit). Valid only once Exited is
// closed.
func ExitErr(p *Pipe) error {
	if p.readErr != nil {
		return fmt.Errorf("reading stream: %w", p.readErr)
	}
	return p.waitErr
}

// WriteLine writes line plus a newline to the child's stdin in one Write.
// Safe for concurrent use.
func WriteLine(p *Pipe, line []byte) error {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	_, err := p.stdin.Write(append(line, '\n'))
	return err
}

// CloseStdin closes the child's stdin, its cue to finish up and exit.
func CloseStdin(p *Pipe) error {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	err := p.stdin.Close()
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}

// Interrupt sends a polite SIGINT to the child's whole session, as the
// interrupt guard does first.
func Interrupt(p *Pipe) { interruptDetached(p.cmd) }

// Kill hard-kills the child's whole session, as the interrupt guard does
// last.
func Kill(p *Pipe) { killDetached(p.cmd) }

// Shutdown ends the child for good: it closes stdin and gives the child
// patience to exit by itself, then sends SIGINT and waits GracePeriod, then
// SIGKILLs. It returns once the child has exited. killed reports whether a
// signal was needed.
func Shutdown(p *Pipe, patience time.Duration) (killed bool) {
	_ = CloseStdin(p)
	select {
	case <-p.exited:
		return false
	case <-time.After(patience):
	}
	if interruptDetached(p.cmd) {
		select {
		case <-p.exited:
			return true
		case <-time.After(gracePeriod):
		}
	}
	killDetached(p.cmd)
	<-p.exited
	return true
}

// Notify subscribes to SIGINT/SIGTERM for as long as the caller is in a call
// it wants to be interruptible, through the same seam as the interrupt guard
// (so tests can inject signals). Call stop when the call ends.
func Notify() (sigs <-chan os.Signal, stop func()) { return interruptChan() }

// GracePeriod is how long to wait after a polite interrupt before
// escalating to SIGKILL.
func GracePeriod() time.Duration { return gracePeriod }

// Interrupted wraps a child's error as ErrInterrupted, the way Capture and
// Stream report an interrupted run.
func Interrupted(err error) error { return interruptedErr(err) }
