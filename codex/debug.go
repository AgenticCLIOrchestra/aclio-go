package codex

import (
	"fmt"

	"github.com/agenticcliorchestra/aclio-go/internal/cliexec"
)

// Debug dumps: every call takes one cliexec.DebugStamp when it starts and
// prefixes all of its files with it — {stamp}-{name}-{kind} — so a plain ls
// lists the files chronologically, with one call's files grouped together and
// the name stating the call's purpose. Names are normalised for filenames via
// cliexec.DebugName, never used raw. All writes are best-effort and no-ops
// when tempDir is empty.

// debugSettings writes the RunOpts as pretty JSON to
// {tempDir}/{stamp}-{name}-settings.json. The prompt is blanked — it is saved
// separately by debugPrompt.
func debugSettings(tempDir, stamp string, opts RunOpts) {
	opts.Prompt = ""
	cliexec.DebugValue(tempDir, stamp+"-"+cliexec.DebugName(opts.Name)+"-settings.json", opts)
}

// debugPrompt writes the prompt to {tempDir}/{stamp}-{name}-prompt.md.
func debugPrompt(tempDir, stamp, name, prompt string) {
	cliexec.DebugString(tempDir, stamp+"-"+cliexec.DebugName(name)+"-prompt.md", prompt)
}

// debugError writes a failed call's error to {tempDir}/{stamp}-{name}-error.txt,
// so a call that produced no output still leaves a trace of what went wrong.
func debugError(tempDir, stamp, name string, callErr error) {
	if callErr == nil {
		return
	}
	cliexec.DebugString(tempDir, stamp+"-"+cliexec.DebugName(name)+"-error.txt", fmt.Sprintf("%v\n", callErr))
}
