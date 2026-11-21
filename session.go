package aclio

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/agenticcliorchestra/aclio-go/claude"
	"github.com/agenticcliorchestra/aclio-go/jsonschema"
)

// ErrSessionEnded is returned (wrapped) by Send / SendStructured when the
// session was closed, or its CLI died and could not be brought back.
var ErrSessionEnded = claude.ErrSessionEnded

// Session is one conversation spanning many turns, whatever the provider:
// open it with StartSession, run turns with Send / SendStructured, and end it
// with CloseSession.
//
// With Claude, the session is one long-lived claude process spoken to over
// stream-json, so later turns skip the process start and transcript reload a
// resumed Run pays. Should that process die, the next turn starts a new one
// resuming the conversation (and a turn the death cut short is sent once
// more). With Codex, every turn is a Run resuming the previous turn's thread,
// so a session behaves exactly like chained Runs.
type Session struct {
	mu      sync.Mutex // held for a whole turn
	req     Request
	id      string // the conversation so far
	started bool   // a claude process has been started at least once
	closed  bool

	// live is the current claude process, nil before the first Claude turn
	// and after one died. Atomic so Interrupt can reach it mid-turn.
	live atomic.Pointer[claude.Session]
}

// StartSession checks req and returns a session for it. req configures every
// turn: Provider, Dir, Model, Name, Stream (implied for Claude), LogWriter,
// Stderr, EventHandlerFactory and the provider extras; ResumeID picks up an
// existing conversation (and ClaudeOpts.ForkSession forks it). req.Prompt
// must be empty — send prompts with Send.
//
// Nothing is launched until the first turn. With Claude, the structured
// output schema is fixed for the whole session: req.JSONSchema if set,
// otherwise the one the first SendStructured generates from its type.
func StartSession(req Request) (*Session, error) {
	if req.Prompt != "" {
		return nil, errors.New("aclio: StartSession takes no Prompt; send it with Send")
	}
	switch req.Provider {
	case Claude, Codex:
	default:
		return nil, fmt.Errorf("unknown provider %q", req.Provider)
	}
	return &Session{req: req, id: req.ResumeID}, nil
}

// Send runs one turn and returns its Result. Result.CostUSD and the token
// counts are the turn's own, so they add up across turns. On ctrl+c the turn
// stops with an error wrapping ErrInterrupted; with Claude the session stays
// usable afterwards.
func Send(s *Session, prompt string) (Result, error) {
	if s.req.Provider == Claude {
		_, res, err := sendClaude[json.RawMessage](s, prompt)
		return res, err
	}
	return sendCodex(s, prompt, Run)
}

// SendStructured runs one turn like Send and decodes its structured output
// into T. When neither the session's JSONSchema nor an earlier turn has fixed
// the schema, it is generated from T.
func SendStructured[T any](s *Session, prompt string) (T, Result, error) {
	var zero T

	s.mu.Lock()
	if s.req.JSONSchema == "" {
		schema, err := jsonschema.FromTypeJSON(zero)
		if err != nil {
			s.mu.Unlock()
			return zero, Result{}, fmt.Errorf("generating schema for %T: %w", zero, err)
		}
		s.req.JSONSchema = string(schema)
	}
	s.mu.Unlock()

	if s.req.Provider == Claude {
		return sendClaude[T](s, prompt)
	}
	var v T
	res, err := sendCodex(s, prompt, func(req Request) (Result, error) {
		var r Result
		var err error
		v, r, err = RunStructured[T](req)
		return r, err
	})
	return v, res, err
}

// sendClaude runs one Claude turn, first starting a claude process resuming
// the conversation if there is none, and retrying once on a fresh one if the
// old one turns out to have died.
func sendClaude[T any](s *Session, prompt string) (T, Result, error) {
	var zero T
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return zero, Result{}, ErrSessionEnded
	}

	for attempt := 0; ; attempt++ {
		cs := s.live.Load()
		if cs == nil {
			opts := claudeOpts(s.req)
			opts.Prompt = ""
			opts.ResumeSessionID = s.id
			// Fork the caller's conversation once, at the start; a restart
			// resumes our own fork.
			opts.ForkSession = opts.ForkSession && !s.started && s.id != ""
			var err error
			if cs, err = claude.Start(s.req.Dir, opts); err != nil {
				return zero, Result{}, err
			}
			s.started = true
			s.live.Store(cs)
		}

		v, parsed, err := claude.SendStructured[T](cs, prompt)
		if id := claude.SessionID(cs); id != "" {
			s.id = id
		}
		if errors.Is(err, claude.ErrSessionEnded) {
			_ = claude.Close(cs)
			s.live.Store(nil)
			if attempt == 0 && !errors.Is(err, ErrInterrupted) {
				continue // the process died: resume in a new one, once
			}
			return zero, Result{}, err
		}
		if err != nil {
			return zero, Result{}, err
		}
		res := claudeResult(parsed)
		res.CostUSD = parsed.TurnCostUSD
		return v, res, nil
	}
}

// sendCodex runs one Codex turn as a Run resuming the previous turn's thread.
func sendCodex(s *Session, prompt string, run func(Request) (Result, error)) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Result{}, ErrSessionEnded
	}
	req := s.req
	req.Prompt = prompt
	req.ResumeID = s.id
	res, err := run(req)
	if err != nil {
		return res, err
	}
	if res.SessionID != "" {
		s.id = res.SessionID
	}
	return res, nil
}

// Interrupt stops the turn in flight, as ctrl+c during Send does, without a
// signal; the interrupted Send returns an error wrapping ErrInterrupted.
// Safe to call from another goroutine while Send runs. With Codex, which can
// only stop a turn by ending its process, it returns an error.
func Interrupt(s *Session) error {
	if s.req.Provider != Claude {
		return errors.New("aclio: Interrupt is supported for Claude sessions only")
	}
	cs := s.live.Load()
	if cs == nil {
		return nil // no process, so no turn in flight
	}
	return claude.Interrupt(cs)
}

// SessionID is the conversation's id so far (Claude session id / Codex thread
// id), for Request.ResumeID: pass it to StartSession or Run to pick the
// conversation up later. It waits for a turn in flight.
func SessionID(s *Session) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.id
}

// CloseSession ends the session, waiting for a turn in flight to finish
// first. Later Sends return ErrSessionEnded. Calling it again is harmless.
func CloseSession(s *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	cs := s.live.Swap(nil)
	if cs == nil {
		return nil
	}
	return claude.Close(cs)
}
