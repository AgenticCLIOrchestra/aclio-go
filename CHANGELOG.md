# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.12.0] - 2026-11-14

### Added
- Event handlers: every line aclio-go logs for a run now passes through an optional handler that returns the lines to write instead, so a consumer can rewrite lines (prefix included), drop them, or hold them back and write them later. Set one process-wide with `aclio.SetEventHandlerFactory`, or per call with `Request.EventHandlerFactory` (also `claude.RunOpts.EventHandlerFactory` / `codex.RunOpts.EventHandlerFactory`). Both take a factory, called once at the start of each run, so each run's handler keeps its own state and needs no locking. Like `LogWriter`, a per-call factory replaces the process-wide one, `Request.EventHandlerFactory` always comes from the `Request`, and a run keeps the factory it started with. The field is excluded from the settings debug dump. With no handler set, logging is unchanged
- `stream` package with the handler types: `Handler`, `HandlerFactory`, `Event` and `Kind`. A run's events arrive in order: `KindStart` (the `[start]` line), one `KindStream` per parsed CLI stream line (only when `Stream` is set), `KindUsage` (the `[cache]` / `[usage]` line, when logged), and always exactly one `KindEnd`, last, even when the run fails. Each `Event` carries the lines aclio-go would write (`Lines`, prefix included), `Provider`, `Name` and `Prefix`, and for stream events `Type`, `Subtype` (for codex `item.*` events, the item's type), `Raw` and `Data`. What a run returns (result, session id, usage) is recorded before the handler sees a line, so a handler can't change it. Aliased from the root package as `aclio.Event`, `aclio.EventHandler` and `aclio.EventHandlerFactory`
- `stream/claudeevent` package (previously internal): typed claude stream events (`AssistantEvent`, `UserEvent`, and the system events `InitEvent`, `PermissionDeniedEvent`, `TaskStartedEvent`, `TaskNotificationEvent`, `ThinkingTokensEvent` and `UnknownEvent`) with `ParseAssistant`, `ParseUser` and `ParseSystem`. For claude stream lines, `stream.Event.Data` holds the line already decoded into these types
- `stream.Provider`, a string-based enum with `stream.ProviderClaude` and `stream.ProviderCodex`

### Changed
- `aclio.Provider` is now an alias of `stream.Provider`, and `aclio.Claude` / `aclio.Codex` are the same values as `stream.ProviderClaude` / `stream.ProviderCodex`, so they compare directly with `Event.Provider`. Existing code is unaffected; only reflection now reports the type as `stream.Provider`

## [0.11.3] - 2026-11-14

### Changed
- Claude stream `user` events are now parsed into typed events internally, so `assistant`, `user` and `system` events are all typed. No public API changes, and log output is unchanged

## [0.11.2] - 2026-11-14

### Changed
- Claude stream `assistant` events are now parsed into typed events internally, alongside the `system` events typed in 0.11.1. No public API changes, and log output is unchanged

## [0.11.1] - 2026-11-14

### Changed
- Claude stream `system` events are now parsed into typed events internally (`init`, `permission_denied`, `task_started`, `task_notification` and `thinking_tokens`). Any other subtype is still accepted, with the whole line kept. No public API changes, and log output is unchanged except for one edge case: an `init` line whose fields don't match the expected shape is now logged raw instead of as the `[system] init` marker, so nothing in it is lost

## [0.11.0] - 2026-11-12

### Added
- Per-call stderr for the CLI: `aclio.Request.Stderr`, `claude.RunOpts.Stderr` and `codex.RunOpts.Stderr` send the `claude` / `codex` process's own stderr (its warnings and errors, separate from this library's `[claude]` / `[codex]` log lines) to their own `io.Writer`. nil keeps `os.Stderr`, so nothing changes unless you set it. Like `LogWriter`, `Request.Stderr` always comes from the `Request`; the `Stderr` inside `Request.ClaudeOpts` / `Request.CodexOpts` is ignored. The field is excluded from the settings debug dump

## [0.10.0] - 2026-11-12

### Added
- Per-call log writers: `aclio.Request.LogWriter`, `claude.RunOpts.LogWriter` and `codex.RunOpts.LogWriter` send one call's `[claude]` / `[codex]` log lines to their own `io.Writer`, so concurrent calls no longer have to share a sink. nil keeps the process-wide sink. Like the other shared fields, `Request.LogWriter` always comes from the `Request`; the `LogWriter` inside `Request.ClaudeOpts` / `Request.CodexOpts` is ignored. The field is excluded from the settings debug dump

### Changed
- `aclio.SetLogWriter` is now safe to call while runs are in flight: the process-wide sink is held atomically, and a run keeps the sink it started with. `SetLogWriter(nil)` restores `os.Stderr` (previously the next log line would panic)

### Fixed
- Data race between `aclio.SetLogWriter` and log lines written by in-flight runs

## [0.9.0] - 2026-11-04

### Added
- `claudeinstall` package — opt-in installer for the Claude Code CLI using Anthropic's official native installer (`https://claude.ai/install.sh` with `bash` on macOS/Linux/WSL, `install.ps1` with PowerShell on Windows). `claudeinstall.Ensure(opts)` returns the path of an existing `claude` binary (on `PATH` or in the installer's default `~/.local/bin`) and installs only if none is found; `claudeinstall.Install(opts)` always runs the installer and returns the installed binary's path. `Opts.Version` picks the target (`stable`, `latest`, or a pinned `x.y.z`; blank means latest, validated before any download), `Opts.Output` receives the installer's output (nil defaults to `os.Stderr`)
- `codexinstall` package — the same API for the Codex CLI using OpenAI's official standalone installer (`https://chatgpt.com/codex/install.sh` with `sh`, `install.ps1` on Windows), run with `CODEX_NON_INTERACTIVE=1` so its prompts can't block. `Opts.Version` accepts `latest` or a pinned `x.y.z` (alpha/beta suffixes allowed) and is passed via `CODEX_RELEASE`; the default install directory honors `CODEX_INSTALL_DIR`, else `~/.local/bin` (unix) / `%LOCALAPPDATA%\Programs\OpenAI\Codex\bin` (Windows)
- When `Ensure` or `Install` finds the binary in the installer's default directory, that directory is moved to the front of the current process's `PATH`, so `claude.Run` / `codex.Run` (and `aclio.Run`) find a freshly installed CLI without a new shell. After `Install`, the fresh copy takes precedence over any other copy already on `PATH`
- Neither package is imported by any other package in this module: nothing is downloaded or installed unless a consumer imports one and calls `Ensure` or `Install`

## [0.8.0] - 2026-10-24

### Changed
- Minimum Go version raised to 1.26 (`go` directive bumped from 1.25.0 to 1.26.0); consumers must build with Go 1.26 or newer
- Bumped `golang.org/x/sys` to v0.49.0 and `golang.org/x/term` to v0.47.0 (and the indirect `golang.org/x/text` to v0.43.0)

## [0.7.0] - 2026-08-29

### Changed
- When `RunOpts.Name` is set, every log line for that run is now led by `[claude] [<name>]` / `[codex] [<name>]` instead of the bare `[claude]` / `[codex]` — the start, streaming-event, and completion (`[cache]` / `[usage]`) lines alike — so a named run's output can be filtered by name (e.g. a workflow step) even when several runs interleave on the same `LogWriter`. Runs with a blank `Name` are unchanged (bare `[claude]` / `[codex]`)

## [0.6.0] - 2026-08-29

### Changed
- Bumped `github.com/santhosh-tekuri/jsonschema/v6` to v6.0.3 (and the indirect `golang.org/x/text` to v0.41.0)

## [0.5.0] - 2026-08-28

### Added
- `claude.RunOpts.ForkSession` — maps to `--fork-session`, continuing a resumed conversation under a new session id so the original session is left untouched; carried through the agnostic layer via `Request.ClaudeOpts`. Valid only alongside `ResumeSessionID` (there is no session to fork otherwise) — `buildArgs` rejects it with an error when set without a resume id

## [0.4.1] - 2026-07-28

### Fixed
- Stream mode read stdout through a 10MB-capped `bufio.Scanner`, so one huge stream-json event (e.g. a claude tool_result echoing a lockfile-sized file) aborted an otherwise healthy run with "token too long"; lines are now read through an unbounded reader, and everything after a huge line still streams normally

## [0.4.0] - 2026-07-28

### Added
- `claude.RunOpts.DisallowedTools` — repeated `--disallowedTools` flags with the same shape validation as `AllowedTools`, carried through the agnostic layer via `Request.ClaudeOpts`. The CLI evaluates deny rules before allow rules, and an exact-match rule (no wildcard) matches only that literal command — so denying `Bash(git fetch)` blocks the bare command while an allow rule `Bash(git fetch:*)` still permits argument-carrying invocations

## [0.3.0] - 2026-07-27

### Added
- `[claude] [start]` / `[codex] [start]` log line announcing each spawn — `op=... model=... prompt_chars=...` — pairing with the `[claude] [cache]` / `[codex] [usage]` completion line via the shared op name; the prompt size is an early tell for runaway payload growth. Emitted after arg validation (a start line means a process actually launched), through the shared `LogWriter` like every other line

## [0.2.0] - 2026-07-27

### Added
- Failed `claude`/`codex` calls now dump their error to `{stamp}-{name}-error.txt` (arg-validation and run failures alike), so a call that produced no output still leaves a trace

### Changed
- Debug dumps (`RunOpts.TempDir`) are now timestamp-prefixed — `{stamp}-{name}-{kind}` with one millisecond-precise stamp per call — so repeated calls with the same name never overwrite each other and `ls` lists them chronologically; the codex `--output-schema` file follows the same naming
- The debug/TempDir directory is created lazily on first write instead of dumps being silently dropped when it doesn't exist
- The prompt is blanked in the dumped `settings.json` — it's already saved separately as `{stamp}-{name}-prompt.md`
- `RunOpts.Name` is normalised for dump filenames instead of used raw — lowercased, anything outside `[a-z0-9_-]` mapped to `-`, empty names become `unnamed`; codex's schema-file `sanitize` is folded into this shared rule

## [0.1.3] - 2026-07-25

### Changed
- `codex` package doc now packs the feature list into its first sentence — live JSONL event-stream parsing with opt-in `[codex]` event/usage logging, schema-backed structured output, session resume, detached-session ctrl+c guard — so pkg.go.dev's synopsis (first sentence only) shows the full picture, aligning it with the `claude` package's doc

## [0.1.2] - 2026-07-25

### Fixed
- `internal/cliexec` tests failed to build on Linux: `syscall.Getsid` is only defined on darwin in the stdlib, so the session-detachment test now uses `golang.org/x/sys/unix.Getsid` (available on all unix platforms); `golang.org/x/sys` is promoted to a direct dependency
- Flaky `TestGuardKillsProcessGroup` on Linux: the group-liveness probe ran before PID 1 reaped the SIGKILLed grandchild (a zombie still counts as a group member), so the probe now polls until the group is gone or a deadline passes

## [0.1.1] - 2026-07-25

### Added
- MIT `LICENSE` file (copyright AgenticCLIOrchestra), so the module is recognized as redistributable and pkg.go.dev can display documentation

## [0.1.0] - 2026-07-08

### Added
- Root `aclio` package — a provider-agnostic front door: a `Provider` enum (`Claude`, `Codex`), a shared `Request`/`Result`, and `Run` / `RunStructured[T]` that route to the selected CLI. Provider-only knobs ride along via `Request.ClaudeOpts` / `Request.CodexOpts`; `SetLogWriter` controls logging for all providers
- `claude` package — a controller for the Claude Code CLI (`claude -p`):
  - `Run` with single-shot (`json`) and streaming (`stream-json`) output modes, session resume (`--resume`), turn limits, system prompt files, and shape-validated model IDs and allowed tools
  - `RunStructured[T]` — all-in-one structured output: generates a JSON schema from `T`'s json tags (unless one is supplied), passes it via `--json-schema`, and decodes `structured_output` back into `T`
  - `ParseStructured[T]`, `ParseResult`, and `Structured` for decoding CLI result blobs, including usage and per-TTL-tier cache metrics
  - Greppable, uniformly `[claude]`-prefixed logging of stream events under a documented taxonomy (`[thinking]` / `[text]` / `[tool] [<Name>]` / `[tool] [denied]` / `[system]` / `[<type>]`), every line trimmed and truncated to keep the stream scannable, plus a per-run `[claude] [cache]` metrics line; unknown system/event subtypes are passed through rather than dropped
  - Optional per-call debug dumps of settings, prompt, and output via `RunOpts.TempDir`
- `codex` package — a controller for the Codex CLI (`codex exec`): parses the JSONL event stream into a `RunResult` (thread id, token usage), supports schema-constrained structured output via `--output-schema` (`RunStructured[T]` / `ParseStructured[T]`), sandbox mode, session resume, and `[codex]`-prefixed event/usage logging
- `prompt` package — parameter substitution for prompt templates with dual rendering: `Render` / `RenderStrict` substitute `{lower_snake_case}` placeholders and produce a compact-JSON rendering for the model and an indented-JSON rendering for debug dumps in one pass. `Text` / `JSON` params; fail-fast render-time errors on dead params, unmarshalable JSON, and zero-value params; leftover-placeholder reporting; values are never re-scanned (injection-proof). Runner-agnostic, stdlib only
- `internal/cliexec` — the shared driver machinery: detached (own-session) process spawning so the CLI can't grab the caller's terminal; an interrupt guard that, on SIGINT/SIGTERM, reaps the detached child's whole tree (polite SIGINT, then SIGKILL after a grace window a second ctrl+c cuts short) and makes the in-flight call return an interrupted sentinel rather than exiting the host process — the embedder decides whether to exit; stdout capture and line-streaming; a configurable `LogWriter`; and best-effort debug dumps. The sentinel is exported as `aclio.ErrInterrupted` / `claude.ErrInterrupted` / `codex.ErrInterrupted` (all the same value; branch with `errors.Is`)
- `jsonschema` package:
  - `FromType` / `FromTypeJSON` / `FromReflectType` — JSON Schema generation from Go types following `encoding/json` tag semantics, with `description:"..."` and `enum:"a,b,c"` struct tags, closed objects (`additionalProperties: false`), and required lists derived from `omitempty`
  - `UnionOf` — merges several payload structs into one flat, all-optional object schema for `{type, data}` discriminated unions, unioning enums and concatenating descriptions
  - `Validate` / `ValidateValue` — JSON Schema (draft 2020-12) validation of raw JSON documents or arbitrary Go values
- `terminal` package — raw-mode `ReadLine` with claude-code-like ergonomics: enter sends, shift+enter inserts a newline (kitty keyboard protocol and xterm modifyOtherKeys), bracketed paste inserted verbatim, ctrl+c returns `ErrInterrupted` instead of killing the process, plain buffered fallback for non-TTY stdin
- CI: PR checks (Go build + test, changelog and version validation) and a post-merge release pipeline (git tag, GitHub release with the version's changelog entry, Go proxy trigger)

[0.12.0]: https://github.com/AgenticCLIOrchestra/aclio-go/compare/v0.11.3...v0.12.0
[0.11.3]: https://github.com/AgenticCLIOrchestra/aclio-go/compare/v0.11.2...v0.11.3
[0.11.2]: https://github.com/AgenticCLIOrchestra/aclio-go/compare/v0.11.1...v0.11.2
[0.11.1]: https://github.com/AgenticCLIOrchestra/aclio-go/compare/v0.11.0...v0.11.1
[0.11.0]: https://github.com/AgenticCLIOrchestra/aclio-go/compare/v0.10.0...v0.11.0
[0.10.0]: https://github.com/AgenticCLIOrchestra/aclio-go/compare/v0.9.0...v0.10.0
[0.9.0]: https://github.com/AgenticCLIOrchestra/aclio-go/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/AgenticCLIOrchestra/aclio-go/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/AgenticCLIOrchestra/aclio-go/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/AgenticCLIOrchestra/aclio-go/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/AgenticCLIOrchestra/aclio-go/compare/v0.4.1...v0.5.0
[0.4.1]: https://github.com/AgenticCLIOrchestra/aclio-go/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/AgenticCLIOrchestra/aclio-go/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/AgenticCLIOrchestra/aclio-go/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/AgenticCLIOrchestra/aclio-go/compare/v0.1.3...v0.2.0
[0.1.3]: https://github.com/AgenticCLIOrchestra/aclio-go/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/AgenticCLIOrchestra/aclio-go/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/AgenticCLIOrchestra/aclio-go/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/AgenticCLIOrchestra/aclio-go/releases/tag/v0.1.0
