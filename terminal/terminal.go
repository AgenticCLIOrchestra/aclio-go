// Package terminal reads console input with claude-code-like ergonomics:
// enter sends, shift+enter inserts a newline (on terminals that can report
// it), pasted text is inserted verbatim, and ctrl+c interrupts the read
// (returning ErrInterrupted) instead of killing the process.
package terminal

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

// ErrInterrupted is returned when the user presses ctrl+c mid-input.
var ErrInterrupted = errors.New("input interrupted")

// stdin is shared across reads so type-ahead buffered during one call isn't
// lost before the next.
var stdin = bufio.NewReader(os.Stdin)

// Terminal extensions enabled around each raw-mode read, in order: bracketed
// paste (pasted text arrives fenced, so its newlines insert instead of send),
// xterm modifyOtherKeys=2 and the kitty keyboard protocol's disambiguate flag
// (both make shift+enter a distinct escape sequence; terminals ignore the one
// they don't speak). Disabled again in reverse order after the read.
const (
	enableExtensions  = "\x1b[?2004h" + "\x1b[>4;2m" + "\x1b[>1u"
	disableExtensions = "\x1b[<u" + "\x1b[>4;0m" + "\x1b[?2004l"
)

// pasteEnd terminates a bracketed paste (everything between CSI 200~ and this
// is pasted content, taken verbatim).
const pasteEnd = "\x1b[201~"

// ReadLine prints prompt and collects one submission from the console with
// claude-code-like behavior: enter alone sends; shift+enter inserts a newline
// (on terminals that can report it — kitty protocol or modifyOtherKeys);
// pasted text is inserted verbatim, its newlines never send. Backspace edits
// the current line; other control sequences (arrows, ...) are ignored.
//
// It returns the submission without a trailing newline. io.EOF means the input
// is exhausted (ctrl+d on an empty line, or a closed stdin); ErrInterrupted
// means ctrl+c. When stdin is not a terminal it degrades to a plain buffered
// line read.
func ReadLine(prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return readLinePlain(prompt)
	}
	return readLineRaw(fd, prompt)
}

// readLinePlain is the non-TTY fallback: one buffered line, enter sends.
func readLinePlain(prompt string) (string, error) {
	fmt.Print(prompt)
	line, err := stdin.ReadString('\n')
	if err != nil {
		if err == io.EOF && line != "" {
			return strings.TrimRight(line, "\r\n"), nil
		}
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// readLineRaw runs the terminal in raw mode for the duration of one
// submission, decoding keys (and echoing, since raw mode turns echo off)
// itself.
func readLineRaw(fd int, prompt string) (string, error) {
	old, err := term.MakeRaw(fd)
	if err != nil {
		return readLinePlain(prompt)
	}
	defer term.Restore(fd, old)

	os.Stdout.WriteString(enableExtensions)
	defer os.Stdout.WriteString(disableExtensions)

	// Raw mode disables output post-processing, so a bare \n no longer implies
	// a carriage return — convert when printing.
	fmt.Print(strings.ReplaceAll(prompt, "\n", "\r\n"))

	// buf accumulates the submission; lineStart indexes where the current
	// display line begins, so backspace never eats past a newline (or the
	// prompt).
	var buf []byte
	lineStart := 0

	for {
		b, err := stdin.ReadByte()
		if err != nil {
			return "", err
		}

		switch {
		case b == 0x03: // ctrl+c
			os.Stdout.WriteString("^C\r\n")
			return "", ErrInterrupted

		case b == 0x04: // ctrl+d: EOF on an empty submission, ignored otherwise
			if len(buf) == 0 {
				os.Stdout.WriteString("\r\n")
				return "", io.EOF
			}

		case b == '\r' || b == '\n': // enter alone: send
			os.Stdout.WriteString("\r\n")
			return string(buf), nil

		case b == 0x7f || b == 0x08: // backspace, within the current line only
			if len(buf) > lineStart {
				_, size := utf8.DecodeLastRune(buf)
				buf = buf[:len(buf)-size]
				os.Stdout.WriteString("\b \b")
			}

		case b == 0x1b:
			params, final, ok := readEscape()
			if !ok {
				continue // lone ESC or non-CSI sequence: ignored
			}
			switch {
			case final == '~' && params == "200": // bracketed paste begins
				pasted, err := readPaste()
				if err != nil {
					return "", err
				}
				buf = appendEcho(buf, pasted)
				lineStart = bytes.LastIndexByte(buf, '\n') + 1
			case final == 'u' && strings.HasPrefix(params, "13;2"), // kitty shift+enter
				final == '~' && params == "27;2;13": // modifyOtherKeys shift+enter
				buf = append(buf, '\n')
				lineStart = len(buf)
				os.Stdout.WriteString("\r\n")
			}
			// Every other sequence (arrows, function keys, ...) is ignored.

		case b >= 0x20: // printable (and UTF-8 continuation) bytes
			buf = append(buf, b)
			os.Stdout.Write([]byte{b})
		}
	}
}

// readEscape consumes the remainder of a CSI escape sequence after its ESC,
// returning the parameter bytes and the final byte. ok is false for a lone ESC
// press or a non-CSI sequence (whose one following byte is swallowed).
func readEscape() (params string, final byte, ok bool) {
	if stdin.Buffered() == 0 {
		return "", 0, false // lone ESC keypress: a real sequence arrives whole
	}
	b, err := stdin.ReadByte()
	if err != nil || b != '[' {
		return "", 0, false
	}

	var p []byte
	for {
		b, err := stdin.ReadByte()
		if err != nil {
			return "", 0, false
		}
		if b >= 0x40 && b <= 0x7e { // the final byte of a CSI sequence
			return string(p), b, true
		}
		p = append(p, b)
	}
}

// readPaste consumes a bracketed paste up to its terminator and returns the
// pasted content verbatim.
func readPaste() (string, error) {
	var content []byte
	for {
		b, err := stdin.ReadByte()
		if err != nil {
			return "", err
		}
		content = append(content, b)
		if bytes.HasSuffix(content, []byte(pasteEnd)) {
			return string(content[:len(content)-len(pasteEnd)]), nil
		}
	}
}

// appendEcho appends pasted text to the buffer with line endings normalised to
// \n, echoing it as it will be kept.
func appendEcho(buf []byte, text string) []byte {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	buf = append(buf, text...)
	os.Stdout.WriteString(strings.ReplaceAll(text, "\n", "\r\n"))
	return buf
}
