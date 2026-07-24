package codex

import (
	"slices"
	"strings"
	"testing"
)

func TestBuildArgs(t *testing.T) {
	args, err := buildArgs(RunOpts{
		Prompt:           "do the thing",
		Model:            "gpt-5.5-codex",
		Sandbox:          SandboxReadOnly,
		SkipGitRepoCheck: true,
		Ephemeral:        true,
		ResumeSessionID:  "abc-123",
	}, "/tmp/schema.json")
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"exec", "--json",
		"resume", "abc-123",
		"--model", "gpt-5.5-codex",
		"--sandbox", "read-only",
		"--skip-git-repo-check",
		"--ephemeral",
		"--output-schema", "/tmp/schema.json",
		"--", "-",
	}
	if !slices.Equal(args, want) {
		t.Errorf("buildArgs =\n  %v\nwant\n  %v", args, want)
	}
}

func TestBuildArgsMinimal(t *testing.T) {
	args, err := buildArgs(RunOpts{Prompt: "hi"}, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"exec", "--json", "--", "-"}
	if !slices.Equal(args, want) {
		t.Errorf("buildArgs = %v, want %v", args, want)
	}
}

func TestBuildArgsPromptNotInArgv(t *testing.T) {
	// The prompt travels on stdin (ARG_MAX), never as an argv element — a
	// prompt that looks like a flag or is huge must not appear in args.
	prompt := "--help please " + strings.Repeat("x", 4096)
	args, err := buildArgs(RunOpts{Prompt: prompt}, "")
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(args, prompt) {
		t.Error("prompt leaked into argv; it must be fed via stdin")
	}
	// stdin is requested via the `-` positional after the `--` guard.
	if args[len(args)-2] != "--" || args[len(args)-1] != "-" {
		t.Errorf("expected trailing `-- -` stdin sentinel, got: %v", args)
	}
}

func TestBuildArgsRejectsBadInput(t *testing.T) {
	if _, err := buildArgs(RunOpts{Prompt: "x", Model: "bad; rm -rf /"}, ""); err == nil {
		t.Error("accepted a model with shell metacharacters")
	}
	if _, err := buildArgs(RunOpts{Prompt: "x", Sandbox: "yolo"}, ""); err == nil {
		t.Error("accepted an invalid sandbox mode")
	}
}

func TestParseStructured(t *testing.T) {
	type out struct {
		Answer string `json:"answer"`
		N      int    `json:"n"`
	}
	v, err := ParseStructured[out](`{"answer": "42", "n": 7}`)
	if err != nil {
		t.Fatal(err)
	}
	if v.Answer != "42" || v.N != 7 {
		t.Errorf("decoded = %+v", v)
	}
	if _, err := ParseStructured[out]("not json"); err == nil {
		t.Error("accepted non-JSON final text")
	}
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"":            "codex",
		"interaction": "interaction",
		"my op/name!": "my-op-name-",
		"weird\tname": "weird-name",
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}
