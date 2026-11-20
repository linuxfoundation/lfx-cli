// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

// Package commands implements the lfx CLI subcommands.
package commands

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"unicode"
	"unicode/utf8"

	"golang.org/x/term"
)

// stdoutIsTerminal reports whether stdout is attached to a terminal. It is a
// variable so tests can force either output path.
var stdoutIsTerminal = func() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// writeResponseOutput copies API response data from r to stdout. Redirected
// or piped output is streamed verbatim, so scripts and binary downloads get
// the exact bytes. Terminal output has control characters escaped (see
// appendTerminalSafe), so response data cannot inject escape sequences that
// rewrite or forge what the user sees. allowCRLF keeps a carriage return that
// ends a line (CR LF) instead of escaping it.
func writeResponseOutput(r io.Reader, allowCRLF bool) error {
	var err error
	if stdoutIsTerminal() {
		err = copyTerminalSafe(os.Stdout, r, allowCRLF)
	} else {
		_, err = io.Copy(os.Stdout, r)
	}
	if err != nil {
		return fmt.Errorf("write response body: %w", err)
	}
	return nil
}

// copyTerminalSafe streams r to w one rune at a time, escaping control
// characters with appendTerminalSafe. Peeking utf8.UTFMax bytes ahead keeps
// multi-byte runes and CR LF pairs intact when they straddle reads.
func copyTerminalSafe(w io.Writer, r io.Reader, allowCRLF bool) error {
	br := bufio.NewReader(r)
	bw := bufio.NewWriter(w)
	out := make([]byte, 0, 8)
	for {
		if br.Buffered() < utf8.UTFMax {
			// The next Peek may block on the network, so first show
			// whatever has already been received.
			if err := bw.Flush(); err != nil {
				return err
			}
		}
		p, err := br.Peek(utf8.UTFMax)
		if err != nil && err != io.EOF {
			// Show the bytes received before the error, as the
			// verbatim io.Copy path would, then report it.
			out = out[:0]
			for len(p) > 0 {
				var n int
				out, n = appendTerminalSafe(out, p, allowCRLF)
				p = p[n:]
			}
			if _, werr := bw.Write(out); werr != nil {
				return werr
			}
			if ferr := bw.Flush(); ferr != nil {
				return ferr
			}
			return err
		}
		if len(p) == 0 {
			return bw.Flush()
		}
		var n int
		out, n = appendTerminalSafe(out[:0], p, allowCRLF)
		if _, err := bw.Write(out); err != nil {
			return err
		}
		if _, err := br.Discard(n); err != nil {
			return err
		}
	}
}

// EscapeTerminalControls returns s with control characters escaped by
// appendTerminalSafe, for text such as error messages that may carry
// server-supplied data (a Goa error's Message, the body of an unexpected
// response, or an OAuth error response). Newlines, tabs, and other Unicode
// are kept.
func EscapeTerminalControls(s string) string {
	p := []byte(s)
	out := make([]byte, 0, len(p))
	for len(p) > 0 {
		var n int
		out, n = appendTerminalSafe(out, p, false)
		p = p[n:]
	}
	return string(out)
}

// appendTerminalSafe appends the first rune of p to dst and returns the
// extended slice and the number of bytes of p consumed. Newline and tab are
// kept, as is CR when allowCRLF is set and the next byte is LF. Other
// control characters (C0, DEL, and C1 U+0080-U+009F) are appended as inert
// escaped text: `\xNN` for ASCII controls, `\uNNNN` for C1. A byte that is
// not valid UTF-8, which includes C1 controls in their 8-bit form, is
// appended as `\xNN`. Every other rune, including valid non-ASCII text, is
// appended unchanged.
func appendTerminalSafe(dst, p []byte, allowCRLF bool) ([]byte, int) {
	r, size := utf8.DecodeRune(p)
	switch {
	case r == utf8.RuneError && size == 1:
		return appendHexEscape(dst, `\x`, uint64(p[0]), 2), size
	case r == '\n' || r == '\t':
		return append(dst, p[0]), size
	case r == '\r' && allowCRLF && len(p) > 1 && p[1] == '\n':
		return append(dst, p[0]), size
	case unicode.IsControl(r) && r < utf8.RuneSelf:
		return appendHexEscape(dst, `\x`, uint64(r), 2), size
	case unicode.IsControl(r):
		return appendHexEscape(dst, `\u`, uint64(r), 4), size
	default:
		return append(dst, p[:size]...), size
	}
}

// appendHexEscape appends prefix followed by v in lowercase hex, zero-padded
// to width digits.
func appendHexEscape(dst []byte, prefix string, v uint64, width int) []byte {
	dst = append(dst, prefix...)
	hex := strconv.FormatUint(v, 16)
	for i := len(hex); i < width; i++ {
		dst = append(dst, '0')
	}
	return append(dst, hex...)
}
