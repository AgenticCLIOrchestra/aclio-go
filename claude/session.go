package claude

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agenticcliorchestra/aclio-go/internal/cliexec"
	"github.com/agenticcliorchestra/aclio-go/stream"
)

// ErrSessionEnded is returned (wrapped) by Send when the session's CLI process
// is gone: it exited or was killed, or the session was closed. Nothing more
// can be sent on it; to carry on, Start a new session with ResumeSessionID set
// to SessionID(s).
var ErrSessionEnded = errors.New("claude session ended")

// ErrSessionBusy is returned by Send when another Send on the same session is
// still in flight. A session runs one turn at a time.
var ErrSessionBusy = errors.New("claude session busy: a turn is already in flight")

// closePatience is how long Close lets the CLI exit by itself after stdin is
// closed (it finishes a turn in flight first), before signals.
var closePatience = 5 * time.Second

// controlTimeout bounds how long Interrupt waits for the CLI's reply.
var controlTimeout = 5 * time.Second

// Session is one long-lived claude CLI process driven over stream-json: each
// Send writes one user message to its stdin and returns the turn's result
// line, without the per-turn process start and transcript reload that
// RunOpts.ResumeSessionID costs. Use it through Start, Send, SendStructured,
// Interrupt, SessionID and Close.
type Session struct {
	opts RunOpts
	pipe *cliexec.Pipe

	// turn receives every stream line that is not a control message, in
	// order. It is closed when the CLI exits.
	turn chan string

	pmu     sync.Mutex
	pending map[string]chan controlReply // request_id -> waiter

	send      sync.Mutex // held by Send for a whole turn
	seq       atomic.Uint64
	sessionID atomic.Pointer[string]

	// totalCost is the CLI's running total_cost_usd as of the last result:
	// the CLI reports the session's cumulative cost, not the turn's. Guarded
	// by send.
	totalCost float64
}

type controlReply struct {
	ok  bool
	err string
}

// Start launches the claude CLI in absDir as a long-lived session. opts
// configures the session as a whole, as it would one Run: model, schema
// (JsonSchema, which then applies to every turn), tools, system prompt file,
// MaxTurns (per turn), ResumeSessionID / ForkSession to pick up an existing
// conversation, and the log writer, stderr, event handler factory and debug
// dir used by each turn. opts.Prompt must be empty — send prompts with Send —
// and opts.Stream is implied: the session always streams.
//
// No turn is run until Send. Close the session when done.
func Start(absDir string, opts RunOpts) (*Session, error) {
	if opts.Prompt != "" {
		return nil, errors.New("claude: Start takes no Prompt; send it with Send")
	}
	opts.Stream = true

	args, err := buildArgs(opts)
	if err != nil {
		return nil, err
	}
	args = append(args, "--input-format", "stream-json")

	debugSettings(opts.TempDir, cliexec.DebugStamp(), opts)

	cmd := cliexec.Command(absDir, "claude", args, opts.Stderr)
	pipe, err := cliexec.Spawn(cmd)
	if err != nil {
		return nil, err
	}

	s := &Session{
		opts:    opts,
		pipe:    pipe,
		turn:    make(chan string, 1024),
		pending: map[string]chan controlReply{},
	}
	if opts.ResumeSessionID != "" && !opts.ForkSession {
		id := opts.ResumeSessionID
		s.sessionID.Store(&id)
	}
	go dispatch(s)
	return s, nil
}

// dispatch sorts the CLI's stdout: replies to our control requests go to
// their waiters, control requests from the CLI are refused (this session
// registers no hooks, permission callback or in-process tools, so none are
// expected, and an unanswered one would hang the turn), and everything else
// goes to the turn in flight.
func dispatch(s *Session) {
	for line := range cliexec.Lines(s.pipe) {
		var env struct {
			Type     string          `json:"type"`
			Request  json.RawMessage `json:"request"`
			ID       string          `json:"request_id"`
			Response struct {
				Subtype string `json:"subtype"`
				ID      string `json:"request_id"`
				Error   string `json:"error"`
			} `json:"response"`
		}
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			s.turn <- line // handleStreamLine skips it
			continue
		}
		switch env.Type {
		case "control_response":
			deliver(s, env.Response.ID, controlReply{ok: env.Response.Subtype == "success", err: env.Response.Error})
		case "control_request":
			var req struct {
				Subtype string `json:"subtype"`
			}
			_ = json.Unmarshal(env.Request, &req)
			_ = writeJSON(s, map[string]any{
				"type": "control_response",
				"response": map[string]any{
					"subtype":    "error",
					"request_id": env.ID,
					"error":      "aclio-go: unsupported control request " + req.Subtype,
				},
			})
		case "control_cancel_request":
			// Nothing of ours to cancel: every request is refused at once.
		default:
			s.turn <- line
		}
	}
	close(s.turn)

	s.pmu.Lock()
	for id, ch := range s.pending {
		ch <- controlReply{err: "session ended"}
		delete(s.pending, id)
	}
	s.pmu.Unlock()
}

func deliver(s *Session, id string, r controlReply) {
	s.pmu.Lock()
	ch, ok := s.pending[id]
	delete(s.pending, id)
	s.pmu.Unlock()
	if ok {
		ch <- r
	}
}

func writeJSON(s *Session, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return cliexec.WriteLine(s.pipe, b)
}

// Send runs one turn: it writes prompt as a user message and returns the
// turn's result line, which ParseResult and Structured decode as for Run.
// Each turn is logged and dumped like one Run: a [start] line, the stream
// events, a [cache] line with the turn's own cost, and debug files under its
// own stamp, all through a handler from the session's factory, called once
// per turn.
//
// The CLI's total_cost_usd is the session's running total; Send logs the
// turn's share, and SendStructured reports it as RunResult.TurnCostUSD.
//
// If SIGINT/SIGTERM arrives during the turn, Send asks the CLI to stop the
// turn, and returns an error wrapping ErrInterrupted once it has: the session
// stays usable. If the CLI hasn't stopped within the grace window, or a
// second signal arrives, the CLI is killed and the session ends. A turn the
// CLI reports as failed is an error. If the CLI is gone, Send returns an
// error wrapping ErrSessionEnded.
func Send(s *Session, prompt string) (string, error) {
	out, _, err := send(s, prompt)
	return out, err
}

// SendStructured runs one turn like Send and decodes its structured_output
// into T. The schema is the session's (RunOpts.JsonSchema given to Start), so
// T must match it.
func SendStructured[T any](s *Session, prompt string) (T, *RunResult, error) {
	var zero T
	out, parsed, err := send(s, prompt)
	if err != nil {
		return zero, nil, err
	}
	var v T
	if err := Structured(out, &v); err != nil {
		return zero, parsed, err
	}
	return v, parsed, nil
}

func send(s *Session, prompt string) (string, *RunResult, error) {
	if !s.send.TryLock() {
		return "", nil, ErrSessionBusy
	}
	defer s.send.Unlock()

	stamp := cliexec.DebugStamp()
	opts := s.opts
	opts.Prompt = prompt

	logger := cliexec.NewLogger(opts.LogWriter, stream.ProviderClaude, opts.Name, opts.EventHandlerFactory)
	defer cliexec.End(logger)
	logStart(opts, logger)
	debugPrompt(opts.TempDir, stamp, opts.Name, prompt)

	out, err := turn(s, prompt, logger)

	var parsed *RunResult
	if out != "" {
		debugOut(opts.TempDir, stamp, opts.Name, out)
		if p, perr := ParseResult(out); perr == nil {
			parsed = p
			parsed.TurnCostUSD = parsed.TotalCostUSD - s.totalCost
			if parsed.TotalCostUSD > 0 {
				s.totalCost = parsed.TotalCostUSD
			}
			if parsed.SessionID != "" {
				id := parsed.SessionID
				s.sessionID.Store(&id)
			}
			if err == nil {
				logUsage(logger, opts.Name, parsed, parsed.TurnCostUSD)
				err = resultErr(parsed)
			}
		} else if err == nil {
			err = fmt.Errorf("parsing claude output: %w", perr)
		}
	}
	debugError(opts.TempDir, stamp, opts.Name, err)
	if err != nil {
		return "", parsed, err
	}
	return out, parsed, nil
}

// resultErr turns a result the CLI marked as failed into an error: one
// wrapping ErrInterrupted when the turn was aborted (see Interrupt).
func resultErr(r *RunResult) error {
	if strings.HasPrefix(r.TerminalReason, "aborted") {
		return fmt.Errorf("%w: turn %s", cliexec.ErrInterrupted, r.TerminalReason)
	}
	if !r.IsError {
		return nil
	}
	msg := r.Subtype
	if len(r.Errors) > 0 {
		msg += ": " + string(r.Errors)
	} else if r.Result != "" {
		msg += ": " + r.Result
	}
	return fmt.Errorf("claude turn failed: %s", msg)
}

// turn writes the user message and reads the stream until the turn's result.
func turn(s *Session, prompt string, logger cliexec.Logger) (string, error) {
	// Anything still queued arrived between turns (e.g. a background task's
	// output). Log it under this turn, but never take a stale result for this
	// turn's.
	for drained := false; !drained; {
		select {
		case line, ok := <-s.turn:
			if !ok {
				return "", endedErr(s)
			}
			var stale string
			handleStreamLine(line, &stale, logger)
		default:
			drained = true
		}
	}

	err := writeJSON(s, map[string]any{
		"type":               "user",
		"message":            map[string]any{"role": "user", "content": prompt},
		"parent_tool_use_id": nil,
		"session_id":         "default",
	})
	if err != nil {
		select {
		case <-cliexec.Exited(s.pipe):
			return "", endedErr(s)
		default:
			return "", fmt.Errorf("writing prompt: %w", err)
		}
	}

	sigs, stop := cliexec.Notify()
	defer stop()

	var interrupted bool
	var grace <-chan time.Time
	for {
		select {
		case line, ok := <-s.turn:
			if !ok {
				if interrupted {
					return "", cliexec.Interrupted(cliexec.ExitErr(s.pipe))
				}
				return "", endedErr(s)
			}
			var result string
			handleStreamLine(line, &result, logger)
			if result == "" {
				continue
			}
			if interrupted {
				// The CLI stopped the turn as asked; the session lives on.
				// The result still comes back, for its cost.
				return result, cliexec.ErrInterrupted
			}
			return result, nil
		case <-sigs:
			if interrupted {
				cliexec.Kill(s.pipe) // second signal: the user is insisting
				continue
			}
			interrupted = true
			go func() { _ = Interrupt(s) }()
			grace = time.After(cliexec.GracePeriod())
		case <-grace:
			cliexec.Kill(s.pipe)
		}
	}
}

func endedErr(s *Session) error {
	<-cliexec.Exited(s.pipe)
	if err := cliexec.ExitErr(s.pipe); err != nil {
		return fmt.Errorf("%w: %v", ErrSessionEnded, err)
	}
	return ErrSessionEnded
}

// Interrupt asks the CLI to stop the turn in flight, as ctrl+c during Send
// does. The interrupted Send returns an error wrapping ErrInterrupted, and
// the session stays usable. Safe to call from another goroutine while Send
// runs; with no turn in flight it does nothing.
func Interrupt(s *Session) error {
	return control(s, map[string]any{"subtype": "interrupt"})
}

// control sends one control request and waits for the CLI's reply.
func control(s *Session, request map[string]any) error {
	var b [4]byte
	_, _ = rand.Read(b[:])
	id := fmt.Sprintf("req_%d_%s", s.seq.Add(1), hex.EncodeToString(b[:]))

	ch := make(chan controlReply, 1)
	s.pmu.Lock()
	s.pending[id] = ch
	s.pmu.Unlock()

	if err := writeJSON(s, map[string]any{"type": "control_request", "request_id": id, "request": request}); err != nil {
		deliver(s, id, controlReply{}) // unregister
		return fmt.Errorf("%w: %v", ErrSessionEnded, err)
	}

	select {
	case r := <-ch:
		if !r.ok {
			return fmt.Errorf("claude control %v: %s", request["subtype"], r.err)
		}
		return nil
	case <-time.After(controlTimeout):
		deliver(s, id, controlReply{}) // unregister
		return fmt.Errorf("claude control %v: no reply within %s", request["subtype"], controlTimeout)
	}
}

// SessionID is the conversation's id: the last result's session_id, or, until
// a turn has run, the ResumeSessionID the session was started with (unless
// forked). Pass it as RunOpts.ResumeSessionID to pick the conversation up in
// a new session or a Run.
func SessionID(s *Session) string {
	if id := s.sessionID.Load(); id != nil {
		return *id
	}
	return ""
}

// Close ends the session: it closes the CLI's stdin, so the CLI finishes any
// turn in flight and exits, and escalates to SIGINT and then SIGKILL if it
// takes too long. It returns once the CLI has exited. Calling it again is
// harmless.
func Close(s *Session) error {
	if killed := cliexec.Shutdown(s.pipe, closePatience); killed {
		return nil
	}
	return cliexec.ExitErr(s.pipe)
}
