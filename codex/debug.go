package codex

import "github.com/agenticcliorchestra/aclio-go/internal/cliexec"

// debugSettings writes the RunOpts as pretty JSON to
// {tempDir}/{name}-settings.json. No-op when tempDir is empty.
func debugSettings(tempDir string, opts RunOpts) {
	cliexec.DebugValue(tempDir, opts.Name+"-settings.json", opts)
}

// debugPrompt writes the prompt to {tempDir}/{name}-prompt.md. No-op when
// tempDir is empty.
func debugPrompt(tempDir, name, prompt string) {
	cliexec.DebugString(tempDir, name+"-prompt.md", prompt)
}
