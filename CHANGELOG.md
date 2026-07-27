# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0] - 2026-07-27

### Added
- Failed `claude`/`codex` calls now dump their error to `{stamp}-{name}-error.txt` (arg-validation and run failures alike), so a call that produced no output still leaves a trace

### Changed
- Debug dumps (`RunOpts.TempDir`) are now timestamp-prefixed — `{stamp}-{name}-{kind}` with one millisecond-precise stamp per call — so repeated calls with the same name never overwrite each other and `ls` lists them chronologically; the codex `--output-schema` file follows the same naming
- The debug/TempDir directory is created lazily on first write instead of dumps being silently dropped when it doesn't exist
- The prompt is blanked in the dumped `settings.json` — it's already saved separately as `{stamp}-{name}-prompt.md`

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

[0.2.0]: https://github.com/AgenticCLIOrchestra/aclio-go/compare/v0.1.3...v0.2.0
[0.1.3]: https://github.com/AgenticCLIOrchestra/aclio-go/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/AgenticCLIOrchestra/aclio-go/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/AgenticCLIOrchestra/aclio-go/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/AgenticCLIOrchestra/aclio-go/releases/tag/v0.1.0
