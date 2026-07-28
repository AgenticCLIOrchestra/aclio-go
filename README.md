# aclio-go

The Go implementation of **ACLIO** — **A**gentic-**CLI**-**O**rchestra.

`aclio-go` drives an agentic *coding CLI* — [Claude Code](https://claude.com/claude-code) (`claude -p`) or [Codex](https://openai.com/codex) (`codex exec`) — as a subprocess from your own Go programs. You hand it a prompt and a working directory; it spawns the CLI, streams and logs the agent's activity, hands back the final text or a **typed struct** decoded from schema-constrained structured output, and lets you resume the session. One provider-agnostic API sits in front of both CLIs so switching between them is a field, not a rewrite.

The name is literal about what it is:

- **a** — agentic
- **cli** — the driver is an agentic CLI (`claude -p`, `codex exec`), **not** a hosted API or SDK. That distinction is the whole point: you get the local CLI's tools, sandboxing, and auth, orchestrated from Go.
- **o** — orchestra / orchestrator: it helps you drive those agents.

The `-go` suffix marks this as the Go implementation, leaving the `AgenticCLIOrchestra` org room for siblings in other languages. (The Go **module** is `aclio-go`; the root **package** you import is `aclio` — so calls read `aclio.Run(...)`.)

This is a personal side project: small wrappers I kept rewriting whenever a Go program needed to orchestrate one of these CLIs. Nothing clever, no framework ambitions — just the glue, done once, with tests. If it saves you an afternoon, great.

```
go get github.com/agenticcliorchestra/aclio-go
```

## Packages

| Package      | What it gives you                                                                                                                           |
|--------------|---------------------------------------------------------------------------------------------------------------------------------------------|
| `aclio` (root) | Provider-agnostic front door: pick a `Provider`, use one `Request`/`Result` and `Run` / `RunStructured[T]`, and it routes to the right CLI |
| `claude`     | Controller for Claude Code (`claude -p`): single-shot and streaming runs, typed structured output, session resume, cache-metrics logging     |
| `codex`      | Controller for Codex (`codex exec`): JSONL event parsing, schema-constrained structured output, sandbox modes, session resume                |
| `prompt`     | Substitute params into a prompt template with fail-fast render-time checking, rendering compact JSON for the model and indented JSON for debug dumps in one pass |
| `jsonschema` | Generate a JSON Schema from any Go type via its `json` tags, build union schemas for discriminated payloads, and validate JSON against a schema |
| `terminal`   | Raw-mode `ReadLine` with claude-code-like ergonomics: shift+enter inserts a newline, paste is verbatim, ctrl+c interrupts the read           |

All the CLI-driving packages share one behavior via `internal/cliexec`: the CLI runs **detached in its own session** so it can't grab your terminal (agentic CLIs disable ISIG, which otherwise eats ctrl+c for your whole process group), and a signal guard makes ctrl+c reap the detached child and return `ErrInterrupted`. See [Interrupts](#interrupts). You get that for free on every run.

## aclio — provider-agnostic front door

Pick a provider; the same types and functions route to it.

```go
res, err := aclio.Run(aclio.Request{
    Provider: aclio.Claude,          // or aclio.Codex
    Dir:      "/abs/workdir",
    Prompt:   "Summarize the open TODOs in this repo.",
    Stream:   true,
})
// res.Text, res.SessionID, res.CostUSD, res.InputTokens, res.OutputTokens
```

Structured output — the schema is generated from your result type and decoded back for you, regardless of provider:

```go
type Triage struct {
    Severity string   `json:"severity" enum:"low,medium,high"`
    Files    []string `json:"files"`
    Summary  string   `json:"summary"`
}

triage, res, err := aclio.RunStructured[Triage](aclio.Request{
    Provider: aclio.Codex,
    Dir:      "/abs/workdir",
    Prompt:   "Triage the failing test.",
})
```

The `Request` common fields (`Prompt`, `Model`, `ResumeID`, `Name`, `Stream`, `JSONSchema`) map to each provider's equivalent. Provider-only knobs ride along without leaving this layer via `Request.ClaudeOpts` / `Request.CodexOpts` (only their *non-shared* fields are consulted — e.g. Claude `AllowedTools` / `DisallowedTools`, Codex `Sandbox`). `aclio.SetLogWriter(w)` redirects logging for every provider at once.

When you need full control, drop to the provider package directly — the root layer is a thin router over them, not a wall.

## claude

```go
out, err := claude.Run("/abs/workdir", claude.RunOpts{
    Name:            "triage",                                 // label for the [claude] [cache] log line
    Prompt:          "Summarize the open TODOs in this repo.", // fed via stdin, so size is a non-issue
    ModelID:         claude.Sonnet,                            // alias, or a pinned ID like claude.Opus48
    AllowedTools:    []string{"Read", "Grep", "Bash(git log:*)"}, // repeated --allowedTools flags
    DisallowedTools: []string{"Bash(git log)"},                // deny rules beat allow rules
    MaxTurns:        10,                                       // 0 = the CLI's default, unlimited
    Stream:          true,                                     // stream-json + live [claude] event logging
})
if err != nil {
    log.Fatal(err)
}
result, err := claude.ParseResult(out)
// result.SessionID, result.Result, result.TotalCostUSD, result.Usage, ...
```

Structured output with the schema generated from your struct's `json` tags:

```go
triage, result, err := claude.RunStructured[Triage]("/abs/workdir", claude.RunOpts{
    Name: "triage", Prompt: "Triage the failing test.", ModelID: claude.Opus,
})
```

`RunStructured` generates the schema from `Triage` (unless `RunOpts.JsonSchema` is set — do that when the schema can't be derived from the type, e.g. discriminated unions behind interface fields), passes it via `--json-schema`, and decodes `structured_output` into the struct. Already holding a result blob? `claude.ParseStructured[Triage](out)` is the decode half on its own.

Set `RunOpts.TempDir` to dump each call's settings/prompt/output/error for debugging — files are named `{stamp}-{name}-{kind}` with one millisecond-precise stamp per call, so repeated calls never overwrite each other and `ls` lists them chronologically.

#### Stream output

With `Stream: true`, every event the CLI emits is logged to the shared writer as **one line, always prefixed `[claude]`** so it stays greppable when interleaved with other terminal output (`grep '^\[claude\]'`). The vocabulary:

| Line | Meaning |
|---|---|
| `[claude] [thinking] <text>` | Thinking block — trimmed, truncated to 200 chars |
| `[claude] [text] <text>` | Assistant text — trimmed, truncated to 200 chars |
| `[claude] [tool] [<Name>] <summary>` | Tool call. One-line summary (truncated to 150) for `Bash`/`Read`/`Edit`/`Write`/`Grep`/`Glob`; any other tool (or a known tool with empty input) falls back to its raw JSON input, truncated to 200 |
| `[claude] [tool] [denied] <content>` | A rejected tool call — content trimmed and truncated to 200 |
| `[claude] [system] init` | The init handshake, collapsed to a marker |
| `[claude] [system] <raw>` | Any other system subtype (e.g. `rate_limit_event`), passed through raw so nothing operationally interesting is dropped |
| `[claude] [<type>]` / `[claude] [<type>] [<subtype>]` | An event type this library doesn't specially render |
| `[claude] [cache] op=… in=… out=… cache_read=… cost_usd=…` | Per-run metrics line (both stream and non-stream modes) |

Malformed lines are skipped silently. This is the human-facing stream; if you need machine-readable events, consume `--output-format stream-json` directly.

## codex

```go
final, result, err := codex.Run("/abs/workdir", codex.RunOpts{
    Name:             "triage",
    Prompt:           "Summarize the open TODOs in this repo.",
    Model:            "gpt-5.5-codex",           // empty = Codex's configured default
    Sandbox:          codex.SandboxReadOnly,     // read-only | workspace-write | danger-full-access
    SkipGitRepoCheck: true,                       // run outside a git repo
    Stream:           true,                        // live [codex] event logging
})
// final is the agent's last message; result.ThreadID / result.Usage
```

Codex takes a schema *file*, so the `codex` package writes your schema to a temp file for `--output-schema` and decodes the agent's final message (the JSON document) into your type:

```go
triage, result, err := codex.RunStructured[Triage]("/abs/workdir", codex.RunOpts{
    Name: "triage", Prompt: "Triage the failing test.", Sandbox: codex.SandboxReadOnly,
})
```

Resume a session by passing a prior `result.ThreadID` as `RunOpts.ResumeSessionID`. Codex reports no per-run cost, so `aclio.Result.CostUSD` is zero for Codex runs.

## prompt

Templating for prompts built from parameters. It does one thing — substitute `{lower_snake_case}` placeholders — but catches the mistakes an ad-hoc `strings.ReplaceAll` loop ships silently, and renders JSON params two ways at once: **compact** for the model (indentation is pure token spend at the wire) and **indented** for debug dumps (humans read those), byte-identical apart from that whitespace.

```go
type Ticket struct {
    Key     string `json:"key"`
    Summary string `json:"summary"`
}

res, err := prompt.Render(templateText, map[string]prompt.Param{
    "instruction": prompt.Text("Summarize the ticket below."),
    "ticket":      prompt.JSON(Ticket{Key: "ABC-1", Summary: "login is broken"}),
})
// res.LLM   → send to the model (JSON compact):  {"key":"ABC-1","summary":"login is broken"}
// res.Debug → dump for humans (JSON indented)
claude.Run(dir, claude.RunOpts{Prompt: res.LLM /* ... */})
```

`prompt.JSON` encodes any Go value (a `string` becomes a JSON string, a struct a JSON object); to substitute already-encoded JSON you're holding, pass it as `json.RawMessage` (or `[]byte`) and it is validated and re-emitted rather than re-encoded — malformed pre-encoded JSON is a render error. Use `prompt.Text` for text substituted verbatim.

`Render` **errors** on a dead param (a key with no `{key}` in the template — a typo or a stale template), on JSON that won't marshal, or on a zero-value `Param`. Placeholder-shaped tokens that survive substitution are reported in `res.Unreplaced` (not an error — templates legitimately mention `{filter}` in prose); use `prompt.RenderStrict` when you own the template and every placeholder must be bound — it turns any leftover (e.g. a `{tikcet}` typo) into a render-time error instead of a broken prompt shipped to the model.

Substituted values are never re-scanned, so a param whose value contains `{other}` can't smuggle in a second substitution — rendering is order-independent and injection-proof. Only `{lower_snake_case}` matches, so JSON/shell examples in the template (`{"k": 1}`, `${VAR}`) are never touched. It's not a template language (no conditionals, loops, or `text/template`) and depends on nothing but the stdlib.

## jsonschema

```go
schema, err := jsonschema.FromType(Triage{})       // map[string]any
data, err  := jsonschema.FromTypeJSON(Triage{})    // []byte, ready for --json-schema / --output-schema
```

Follows `encoding/json` semantics: tag names, `-` skips, `,string` becomes a string, anonymous embedded structs are flattened, unexported fields ignored. Extras:

- fields **without** `omitempty` land in `required`
- every object gets `additionalProperties: false`
- a `description:"..."` struct tag becomes the property description
- an `enum:"a,b,c"` struct tag constrains the property to those values
- `time.Time` → `{"type": "string", "format": "date-time"}`, `[]byte` → base64 string, `any`/`json.RawMessage` → unconstrained
- recursive types return an error instead of looping

The output deliberately matches the schema dialect the CLIs' structured outputs accept (closed objects, no recursion), so `FromTypeJSON` output feeds straight into `--json-schema` / `--output-schema` (or an API `output_config.format`).

### Union schemas for discriminated payloads

For `{type, data}` envelopes where which of `data`'s fields are valid depends on `type` (and a decoder enforces it), `UnionOf` merges several payload structs into one flat, all-optional object schema:

```go
data, err := jsonschema.UnionOf(MouseAction{}, KeyboardAction{}, WaitAction{})
// one object schema holding the union of all fields; same-name fields must
// agree on type, enums are unioned, descriptions concatenated
```

### Validation

```go
err := jsonschema.Validate(schemaJSON, docJSON)     // raw JSON vs raw JSON
err := jsonschema.ValidateValue(schemaJSON, value)  // any Go value, marshalled then checked
```

Backed by [santhosh-tekuri/jsonschema](https://github.com/santhosh-tekuri/jsonschema) (draft 2020-12). `nil` means valid.

## terminal

```go
input, err := terminal.ReadLine("> ")
switch {
case errors.Is(err, terminal.ErrInterrupted): // ctrl+c — clean up, don't die
case errors.Is(err, io.EOF):                  // ctrl+d on empty line / closed stdin
}
```

Runs the terminal in raw mode for the duration of one submission: enter sends, shift+enter inserts a newline (kitty keyboard protocol and xterm modifyOtherKeys both supported; other terminals ignore it), bracketed paste inserts multi-line text without sending, backspace edits within the current line. Falls back to a plain buffered read when stdin isn't a TTY.

## Interrupts

Agentic CLIs grab the controlling terminal even in print mode — they disable ISIG, so a ctrl+c is swallowed for your whole process group while the child runs. To stay interruptible, aclio-go runs the child **detached in its own session** (unix) / process group (windows), then installs a guard for the child's lifetime. On SIGINT/SIGTERM the guard reaps the child's whole tree — a polite SIGINT first, then SIGKILL after a short grace window (a second ctrl+c skips the wait) — and the in-flight `Run` call **returns an error wrapping `ErrInterrupted`** (exported as `aclio.ErrInterrupted` / `claude.ErrInterrupted` / `codex.ErrInterrupted` — all the same value). It does *not* exit your process: deferred cleanup runs, and one interrupted call is a recoverable error.

```go
_, err := claude.Run(dir, opts)
if errors.Is(err, claude.ErrInterrupted) { // also aclio.ErrInterrupted / codex.ErrInterrupted
    // an embedding library treats this as recoverable; a CLI front-end
    // typically does its cleanup then os.Exit(130) — the exit decision
    // belongs to the binary, not this library.
}
```

**Windows caveat:** there's no session-wide kill via a negative pid and no way to deliver a polite ctrl+c to another group, so on windows the guard skips the grace window and immediately hard-terminates the **direct child only** — grandchildren the CLI spawned may survive. Unix sends the polite SIGINT, waits the grace, then reaps the whole session.

## Requirements

- Go 1.25+
- The relevant CLI on `PATH`: `claude` for the `claude` package, `codex` for the `codex` package (the root `aclio` package needs whichever providers you actually invoke). `jsonschema` and `terminal` have no external dependency.

## Versioning & releases

[SemVer](https://semver.org), tracked in the `VERSION` file, with a [Keep a Changelog](https://keepachangelog.com) `CHANGELOG.md`. Merges to master tag `v{VERSION}` and publish a GitHub release with that version's changelog entry; CI enforces the version bump and changelog format on every PR.

Pre-1.0: the API may still move between minor versions.

## License

MIT
