package vt10x

import (
	"io"
	"strings"
	"testing"
)

func extractStr(term Terminal, x0, x1, row int) string {
	var s []rune
	for i := x0; i <= x1; i++ {
		attr := term.Cell(i, row)
		s = append(s, attr.Char)
	}
	return string(s)
}

func TestPlainChars(t *testing.T) {
	term := New()
	expected := "Hello world!"
	_, err := term.Write([]byte(expected))
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	actual := extractStr(term, 0, len(expected)-1, 0)
	if expected != actual {
		t.Fatal(actual)
	}
}

func TestNewline(t *testing.T) {
	term := New()
	expected := "Hello world!\n...and more."
	_, err := term.Write([]byte("\033[20h")) // set CRLF mode
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	_, err = term.Write([]byte(expected))
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}

	split := strings.Split(expected, "\n")
	actual := extractStr(term, 0, len(split[0])-1, 0)
	actual += "\n"
	actual += extractStr(term, 0, len(split[1])-1, 1)
	if expected != actual {
		t.Fatal(actual)
	}

	// A newline with a color set should not make the next line that color,
	// which used to happen if it caused a scroll event.
	st := (term.(*terminal))
	st.moveTo(0, st.rows-1)
	_, err = term.Write([]byte("\033[1;37m\n$ \033[m"))
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	cur := term.Cursor()
	attr := term.Cell(cur.X, cur.Y)
	if attr.FG != DefaultFG {
		t.Fatal(st.cur.X, st.cur.Y, attr.FG, attr.BG)
	}
}

func FuzzTerminal(f *testing.F) {
	testcases := []string{
		// plain text
		"Hello, world", " ", "!12345",
		// cursor movement
		"\033[A", "\033[B", "\033[C", "\033[D",
		"\033[10;5H", "\033[2J", "\033[K",
		// SGR colors and attributes
		"\033[1;31mRed Bold\033[0m",
		"\033[38;5;196mColor256\033[m",
		"\033[38;2;255;128;0mRGB\033[m",
		// common PTY sequences
		"\033[?1049h\033[?1049l", // alt screen on/off
		"\033[?25l\033[?25h",     // cursor hide/show
		"\033[?1h\033[?1l",       // application cursor keys
		"\033(B\033)0",           // charset designation
		// scroll regions and insert/delete
		"\033[5;20r", "\033[1L", "\033[1M", "\033[1P", "\033[1@",
		// window title and OSC
		"\033]0;title\007",
		"\033]2;window\033\\",
		// control characters
		"\r\n", "\t", "\b", "\007",
		// UTF-8
		"こんにちは", "你好", "🐹",
		// erase display: all variants (J)
		"\033[0J", "\033[1J", "\033[2J", "\033[3J",
		// erase line: all variants (K)
		"\033[0K", "\033[1K", "\033[2K",
		// erase n chars (X)
		"\033[5X", "\033[0X",
		// cursor next/prev line (E, F)
		"\033[3E", "\033[3F",
		// vertical position absolute (d)
		"\033[12d",
		// horizontal position absolute (G, `)
		"\033[40G", "\033[40`",
		// tab stop set/clear (ESC H, CSI g)
		"\033H", "\033[0g", "\033[3g",
		// scroll up/down (S, T)
		"\033[3S", "\033[3T",
		// insert/delete lines (L, M)
		"\033[5L", "\033[5M",
		// index / reverse index / next line (ESC D, M, E)
		"\033D", "\033M", "\033E",
		// full reset (ESC c)
		"\033c",
		// save/restore cursor (ESC 7/8 and CSI s/u)
		"\0337", "\0338", "\033[s", "\033[u",
		// device status report / cursor position report (CSI n)
		"\033[5n", "\033[6n",
		// bracketed paste mode
		"\033[?2004h\033[200~pasted text\033[201~\033[?2004l",
		// focus events
		"\033[?1004h", "\033[?1004l",
		// mouse tracking modes
		"\033[?1000h", "\033[?1002h", "\033[?1003h", "\033[?1006h",
		"\033[?1000l", "\033[?1002l", "\033[?1003l", "\033[?1006l",
		// DEC line drawing charset
		"\033(0lqkxjmwuvt\033(B",
		// realistic shell prompt
		"\033[1;32muser@host\033[0m:\033[1;34m~/code\033[0m$ ",
		// typical vim-style sequence: alt screen + cursor ops
		"\033[?1049h\033[2J\033[H\033[?25l\033[?25h\033[?1049l",
		// line wrap: write to last column to trigger wrap logic
		"\033[1;80H" + "A" + "\033[1;1H",

		// invalid: zero-size args
		"\033[0@", "\033[0I", "\033[0Z",
		// invalid: missing final byte (truncated CSI)
		"\033[", "\033[1", "\033[1;",
		// invalid: semicolons only
		"\033[;H", "\033[;;H", "\033[;1H",
		// invalid: unknown CSI final bytes
		"\033[1q", "\033[1y", "\033[1~",
		// invalid: unknown ESC sequences
		"\033Q", "\033X", "\033^foo\007",
		// invalid: scroll region inverted (bottom < top)
		"\033[20;5r",
		// invalid: large args for scroll, insert/delete lines, erase
		"\033[999999S", "\033[999999T",
		"\033[999999L", "\033[999999M",
		"\033[999999X",
		// previously-panicking: insertBlanks/deleteChars with negative args
		"\x1b[-1@", "\x1b[-1P",
		// previously-panicking: insertBlanks/deleteChars with int64 overflow
		"\x1b[9223372036854775807@", "\x1b[9223372036854775807P",
		// previously-panicking: CHT/CBT with MaxInt causing infinite loop
		"\x1b[1;1H\x1b[9223372036854775807I",
		"\x1b[1;80H\x1b[9223372036854775807Z",
		// cursor near right margin + large insert/delete
		"\x1b[1;79H\x1b[999999@", "\x1b[1;79H\x1b[999999P",
		// invalid: OSC without terminator
		"\033]0;unterminated title",
		// invalid: CSI buffer overflow (>256 bytes)
		"\033[" + strings.Repeat("1;", 130) + "H",
		// invalid: null bytes embedded
		"\x1b[1;1H\x00\x00text\x00",
		// invalid: negative CUP coords
		"\x1b[-5;-5H",
		// invalid: VPA past screen
		"\033[9999d",

		// write-back paths: OSC color queries and DSR/CPR
		// FuzzTerminal uses New() with ioutil.Discard so no nil-writer panic here;
		// seeds exercise the OSC/DSR/CPR code paths for general coverage
		"\033]10;?\007",          // OSC fg color query
		"\033]11;?\007",          // OSC bg color query
		"\033]4;1;?\007",         // OSC palette color query
		"\033[5n",                // DSR
		"\033[6n",                // CPR
		// arithmetic overflow: INT_MAX arg for each cursor/erase command
		"\033[9223372036854775807X", // ECH INT_MAX
		"\033[9223372036854775807A", // CUU INT_MAX
		"\033[9223372036854775807B", // CUD INT_MAX
		"\033[9223372036854775807C", // CUF INT_MAX
		"\033[9223372036854775807D", // CUB INT_MAX
		"\033[0d",                   // VPA arg=0
		// non-ASCII byte inside CSI (U+0148 low byte = 'H' = CUP without fix)
		"\033[\xc5\x88",
	}
	for _, tc := range testcases {
		f.Add([]byte(tc))
	}
	terminal := New()
	f.Fuzz(func(t *testing.T, orig []byte) {
		_, err := terminal.Write(orig)
		if err != nil {
			t.Fatalf("error not expected, got %v", err)
		}
	})
}
