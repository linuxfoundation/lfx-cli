// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package commands

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"testing/iotest"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and with
// stdoutIsTerminal forced to isTerminal, returning what fn wrote.
func captureStdout(t *testing.T, isTerminal bool, fn func() error) []byte {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStdout, origIsTerminal := os.Stdout, stdoutIsTerminal
	os.Stdout = w
	stdoutIsTerminal = func() bool { return isTerminal }
	t.Cleanup(func() {
		os.Stdout, stdoutIsTerminal = origStdout, origIsTerminal
	})

	done := make(chan []byte)
	go func() {
		data, _ := io.ReadAll(r)
		done <- data
	}()
	fnErr := fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	out := <-done
	if fnErr != nil {
		t.Fatalf("unexpected error: %v", fnErr)
	}
	return out
}

func TestCopyTerminalSafe(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		allowCRLF bool
		want      string
	}{
		{name: "plain text and unicode kept", in: "{\"a\": \"ā 中文 é\"}\n\tok", want: "{\"a\": \"ā 中文 é\"}\n\tok"},
		{name: "C0 controls escaped", in: "a\x1b[2Jb\x07c\x00d", want: `a\x1b[2Jb\x07c\x00d`},
		{name: "DEL escaped", in: "a\x7fb", want: `a\x7fb`},
		{name: "UTF-8 C1 escaped", in: "a\u009bb\u0085c", want: `a\u009bb\u0085c`},
		{name: "8-bit C1 and invalid bytes escaped", in: "a\x9bb\xffc", want: `a\x9bb\xffc`},
		{name: "truncated rune at end escaped", in: "a\xc2", want: `a\xc2`},
		{name: "CRLF kept in raw mode", in: "a\r\nb", allowCRLF: true, want: "a\r\nb"},
		{name: "lone CR escaped in raw mode", in: "a\rb\r", allowCRLF: true, want: `a\x0db\x0d`},
		{name: "CR escaped in query mode", in: "a\r\nb", want: "a\\x0d\nb"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// OneByteReader splits every multi-byte rune and CR LF pair
			// across reads, exercising the streaming lookahead.
			var buf bytes.Buffer
			if err := copyTerminalSafe(&buf, iotest.OneByteReader(strings.NewReader(tc.in)), tc.allowCRLF); err != nil {
				t.Fatalf("copyTerminalSafe: %v", err)
			}
			if buf.String() != tc.want {
				t.Errorf("got %q, want %q", buf.String(), tc.want)
			}
		})
	}
}

func TestCopyTerminalSafeReadError(t *testing.T) {
	// A read error with fewer than utf8.UTFMax bytes buffered must still
	// write those bytes (escaped) before returning the error.
	boom := errors.New("boom")
	var buf bytes.Buffer
	err := copyTerminalSafe(&buf, io.MultiReader(strings.NewReader("ok\x1b"), iotest.ErrReader(boom)), true)
	if !errors.Is(err, boom) {
		t.Fatalf("copyTerminalSafe error = %v, want %v", err, boom)
	}
	if want := `ok\x1b`; buf.String() != want {
		t.Errorf("output before error = %q, want %q", buf.String(), want)
	}
}

func TestWriteQueryOutputTerminal(t *testing.T) {
	body := []byte(`{"name":"a\u001b]0;x\u0007b\rc\u009bd ā"}`)

	got := captureStdout(t, true, func() error { return writeQueryOutput(body, "name") })
	if want := `a\x1b]0;x\x07b\x0dc\u009bd ā`; string(got) != want {
		t.Errorf("terminal output = %q, want %q", got, want)
	}

	got = captureStdout(t, false, func() error { return writeQueryOutput(body, "name") })
	if want := "a\x1b]0;x\x07b\rc\u009bd ā"; string(got) != want {
		t.Errorf("redirected output = %q, want exact decoded bytes %q", got, want)
	}
}

func TestWriteResponseOutput(t *testing.T) {
	body := []byte("line1\r\n\x1b[1Aforged\xc2\x9b\x9b ok ā\n")

	got := captureStdout(t, true, func() error { return writeResponseOutput(bytes.NewReader(body), true) })
	if want := "line1\r\n\\x1b[1Aforged\\u009b\\x9b ok ā\n"; string(got) != want {
		t.Errorf("terminal output = %q, want %q", got, want)
	}

	got = captureStdout(t, false, func() error { return writeResponseOutput(bytes.NewReader(body), true) })
	if !bytes.Equal(got, body) {
		t.Errorf("redirected output = %q, want body unchanged %q", got, body)
	}
}

func TestEscapeTerminalControls(t *testing.T) {
	// A Goa error for an unexpected status embeds the raw response body.
	in := "invalid response code 500, body: \x1b]52;c;aGk=\x07fake\r\u009b\nā\tok"
	if want := "invalid response code 500, body: \\x1b]52;c;aGk=\\x07fake\\x0d\\u009b\nā\tok"; EscapeTerminalControls(in) != want {
		t.Errorf("EscapeTerminalControls = %q, want %q", EscapeTerminalControls(in), want)
	}
}
