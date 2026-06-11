package vt10x

import (
	"bytes"
	"fmt"
	"math"
	"testing"
	"time"
)

// TestNewZeroSizeNoPanic ensures constructing a terminal with a zero size does not panic. resize(0,0) is a no-op so the
// internal state stays at cols=0/rows=0; reset() must tolerate that.
func TestNewZeroSizeNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("New(WithSize(0,0)) panicked: %v", r)
		}
	}()

	vt := New(WithSize(0, 0))
	cols, rows := vt.Size()
	if cols != 0 || rows != 0 {
		t.Fatalf("expected 0x0 terminal, got %dx%d", cols, rows)
	}
}

func TestZeroSizeWriteNoPanic(t *testing.T) {
	vt := New(WithSize(0, 0))
	sequences := []string{
		"plain text",
		"\x1bH",
		"\x1b[0g",
		"\x1b[6n",
		"\x1b]0;title\x07",
	}

	for _, seq := range sequences {
		t.Run(fmt.Sprintf("%q", seq), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Write(%q) on zero-size terminal panicked: %v", seq, r)
				}
			}()

			if _, err := vt.Write([]byte(seq)); err != nil {
				t.Fatalf("Write(%q) returned error: %v", seq, err)
			}
		})
	}
}

func TestZeroSizeWriteNoPanicSimple(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Write on 0-size terminal panicked: %v", r)
		}
	}()
	vt := New(WithSize(0, 0))
	vt.Write([]byte("hello\033[H\033[2J"))
}

func TestZeroSizeAltScreenNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("alt-screen on 0-size terminal panicked: %v", r)
		}
	}()
	vt := New(WithSize(0, 0))
	vt.Write([]byte("\033[?1049h\033[?1049l\033[?47h\033[?47l"))
}

func TestNilWriterNoPanic(t *testing.T) {
	vt := New(WithWriter(nil))
	sequences := []string{
		"\x1b[5n",
		"\x1b[6n",
		"\x1b]10;?\x07",
		"\x1b]11;?\x07",
	}

	for _, seq := range sequences {
		t.Run(fmt.Sprintf("%q", seq), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Write(%q) with nil writer panicked: %v", seq, r)
				}
			}()

			if _, err := vt.Write([]byte(seq)); err != nil {
				t.Fatalf("Write(%q) returned error: %v", seq, err)
			}
		})
	}
}

func TestZeroStateMutationHelpersNoPanic(t *testing.T) {
	s := newState(nil)
	steps := []struct {
		name string
		fn   func()
	}{
		{"putTab", func() { s.putTab(true) }},
		{"clear", func() { s.clear(0, 0, 1, 1) }},
		{"moveTo", func() { s.moveTo(10, 10) }},
		{"scrollUp", func() { s.scrollUp(0, 1, true) }},
		{"scrollDown", func() { s.scrollDown(0, 1) }},
		{"insertBlanks", func() { s.insertBlanks(1) }},
		{"deleteChars", func() { s.deleteChars(1) }},
		{"oscColorResponse", func() { s.oscColorResponse(int(DefaultFG), 10) }},
		{"osc4ColorResponse", func() { s.osc4ColorResponse(1) }},
	}

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s panicked on zero state: %v", step.name, r)
				}
			}()
			step.fn()
		})
	}
}

func TestNilWriterFallsBackToDiscard(t *testing.T) {
	var buf bytes.Buffer
	vt := New(WithWriter(&buf))
	if _, err := vt.Write([]byte("\x1b[5n")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected DSR response to be written")
	}

	vt = New(WithWriter(nil))
	if _, err := vt.Write([]byte("\x1b[5n")); err != nil {
		t.Fatalf("Write returned error with nil writer: %v", err)
	}
}

// TestResizeRejectsExtremeSizes ensures pathological resize requests are  rejected rather than blindly allocating
// terabytes. The library is used to replay untrusted session recordings, so a malformed resize event must not be able
// to OOM the auth server.
func TestResizeRejectsExtremeSizes(t *testing.T) {
	cases := []struct{ cols, rows int }{
		{math.MaxInt, math.MaxInt},
		{math.MaxInt, 24},
		{80, math.MaxInt},
		{maxResizeDim + 1, 24},
		{80, maxResizeDim + 1},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%dx%d", tc.cols, tc.rows), func(t *testing.T) {
			vt := New(WithSize(80, 24))

			done := make(chan struct{})
			go func() {
				defer close(done)
				vt.Resize(tc.cols, tc.rows)
			}()

			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatalf("Resize(%d,%d) did not return", tc.cols, tc.rows)
			}

			cols, rows := vt.Size()
			if cols > maxResizeDim || rows > maxResizeDim {
				t.Fatalf("Resize(%d,%d) produced %dx%d, exceeds cap %d",
					tc.cols, tc.rows, cols, rows, maxResizeDim)
			}
		})
	}
}

func TestECHOverflowNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ECH overflow panicked: %v", r)
		}
	}()
	vt := New(WithSize(80, 24))
	// Position at col 40 (1-indexed = col 39 0-indexed), then ECH with INT_MAX
	seq := fmt.Sprintf("\033[1;40H\033[%dX", math.MaxInt)
	vt.Write([]byte(seq))
	cur := vt.Cursor()
	// Cursor must stay at col 39 (0-indexed), not move to 0
	if cur.X != 39 {
		t.Errorf("cursor at col %d after ECH, want 39", cur.X)
	}
}

func TestCursorMoveOverflowNoPanic(t *testing.T) {
	cases := []struct {
		name         string
		seq          string
		wantX, wantY int
	}{
		{"CUU INT_MAX", fmt.Sprintf("\033[12;40H\033[%dA", math.MaxInt), 39, 0},
		{"CUD INT_MAX", fmt.Sprintf("\033[12;40H\033[%dB", math.MaxInt), 39, 23},
		{"CUF INT_MAX", fmt.Sprintf("\033[12;40H\033[%dC", math.MaxInt), 79, 11},
		{"CUB INT_MAX", fmt.Sprintf("\033[12;40H\033[%dD", math.MaxInt), 0, 11},
		{"CNL INT_MAX", fmt.Sprintf("\033[12;40H\033[%dE", math.MaxInt), 0, 23},
		{"CPL INT_MAX", fmt.Sprintf("\033[12;40H\033[%dF", math.MaxInt), 0, 0},
		{"VPA 0", "\033[0d", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s panicked: %v", tc.name, r)
				}
			}()
			vt := New(WithSize(80, 24))
			vt.Write([]byte(tc.seq))
			cur := vt.Cursor()
			if cur.X != tc.wantX || cur.Y != tc.wantY {
				t.Errorf("cursor at (%d,%d), want (%d,%d)", cur.X, cur.Y, tc.wantX, tc.wantY)
			}
		})
	}
}

// TestNonASCIIInCSI verifies that a non-ASCII rune whose low byte would dispatch
// a CSI command (e.g. U+0148 low byte = 0x48 = 'H' = CUP) is discarded instead.
func TestNonASCIIInCSI(t *testing.T) {
	vt := New(WithSize(80, 24))
	// Position cursor at col 5, row 5 (1-indexed: row 6, col 6)
	vt.Write([]byte("\033[6;6H"))
	cur := vt.Cursor()
	if cur.X != 5 || cur.Y != 5 {
		t.Fatalf("setup failed: cursor at (%d,%d), want (5,5)", cur.X, cur.Y)
	}
	// U+0148 encodes as \xc5\x88 in UTF-8; byte(0x0148) = 0x48 = 'H'
	// Without the fix this dispatches CUP with no args → moves cursor to (0,0)
	vt.Write([]byte("\033[\xc5\x88"))
	cur = vt.Cursor()
	if cur.X != 5 || cur.Y != 5 {
		t.Errorf("non-ASCII in CSI moved cursor to (%d,%d), want (5,5)", cur.X, cur.Y)
	}
}

// FuzzWrite throws arbitrary bytes at a freshly constructed terminal and  fails on any panic. This is the safety net
// for session-recording replay: a corrupt/truncated stream from a misbehaving storage backend must not  be able to
// crash auth.
func FuzzWrite(f *testing.F) {
	seeds := []string{
		// Plain text.
		"hello world",
		// Known previously-panicking CSI sequences.
		"\x1b[-1@",
		"\x1b[-1P",
		"\x1b[9223372036854775807@",
		"\x1b[9223372036854775807P",
		"\x1b[1;1H\x1b[9223372036854775807I",
		"\x1b[1;80H\x1b[9223372036854775807Z",
		// Long runs of CSI parameters.
		"\x1b[1;2;3;4;5;6;7;8;9;10;11;12;13;14H",
		// OSC color commands.
		"\x1b]4;1;rgb:ff/00/00\x07",
		// Mixed printable + control.
		"abc\x1b[31mred\x1b[0m\x1b[2J\x1b[H",
		// Tab stops + resize sequences.
		"\x1bH\x1b[8;24;80t",
		// Scroll region + oversized scroll, exercising scrollback capture clamping.
		"\x1b[2;4r\x1b[99S",
	}

	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		vt := New(WithSize(80, 24), WithScrollbackCapture(32))
		done := make(chan struct{})

		go func() {
			defer close(done)

			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Write(%q) panicked: %v", data, r)
				}
			}()

			_, _ = vt.Write(data)
		}()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("Write(%q) did not complete within timeout", data)
		}
	})
}
