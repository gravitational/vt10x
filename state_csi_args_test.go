package vt10x

import (
	"fmt"
	"math"
	"testing"
	"time"
)

// TestCSICharOpsBadArg ensures insertBlanks and deleteChars do not panic on malformed CSI arguments (negative values or
// values large enough to cause signed integer overflow in dst = cur.X + n or src = cur.X + n).
func TestCSICharOpsBadArg(t *testing.T) {
	ns := []struct {
		name string
		n    int
	}{
		{"negative small", -1},
		{"negative large", -1_000_000},
		{"zero", 0},
		{"one", 1},
		{"at cols", 80},
		{"just past cols", 81},
		{"much past cols", 1_000_000},
		{"max int overflow", math.MaxInt},
		{"min int underflow", math.MinInt},
	}

	ops := []struct {
		name string
		fn   func(*State, int)
	}{
		{"insertBlanks", (*State).insertBlanks},
		{"deleteChars", (*State).deleteChars},
	}

	for _, op := range ops {
		for _, tc := range ns {
			t.Run(op.name+"/"+tc.name, func(t *testing.T) {
				s := newState(nil)
				s.resize(80, 24)
				s.reset()
				s.moveTo(5, 5)

				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s(%d) panicked: %v", op.name, tc.n, r)
					}
				}()

				op.fn(s, tc.n)
			})
		}
	}
}

// TestCSITabLoopsBounded ensures CHT (CSI I) and CBT (CSI Z) don't spin the goroutine when given a pathologically large
// argument. putTab is bounded by cols, so looping more than cols times is wasted work; an adversarial stream with
// INT_MAX wouldn't panic but would hang the processor.
func TestCSITabLoopsBounded(t *testing.T) {
	sequences := []string{
		// Move to (1, 1), then CHT with INT_MAX.
		fmt.Sprintf("\x1b[1;1H\x1b[%dI", math.MaxInt),
		// Move to last column, then CBT with INT_MAX.
		fmt.Sprintf("\x1b[1;80H\x1b[%dZ", math.MaxInt),
	}

	for _, seq := range sequences {
		t.Run(fmt.Sprintf("%q", seq), func(t *testing.T) {
			done := make(chan struct{})
			go func() {
				defer close(done)
				vt := New()
				vt.Resize(80, 24)
				_, _ = vt.Write([]byte(seq))
			}()

			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatalf("Write(%q) did not complete within timeout", seq)
			}
		})
	}
}

// TestCellOutOfBounds ensures Cell returns a zero Glyph rather than panicking when given out-of-range coordinates.
// This makes the library safe-by-default for downstream callers that might otherwise trust an externally-supplied offset.
func TestCellOutOfBounds(t *testing.T) {
	s := newState(nil)
	s.resize(80, 24)
	s.reset()

	cases := []struct{ x, y int }{
		{-1, 0},
		{0, -1},
		{-1, -1},
		{80, 0},
		{0, 24},
		{80, 24},
		{1_000_000, 0},
		{0, 1_000_000},
		{math.MaxInt, math.MaxInt},
		{math.MinInt, math.MinInt},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d,%d", tc.x, tc.y), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Cell(%d,%d) panicked: %v", tc.x, tc.y, r)
				}
			}()

			got := s.Cell(tc.x, tc.y)
			if got != (Glyph{}) {
				t.Fatalf("Cell(%d,%d) = %+v, want zero Glyph", tc.x, tc.y, got)
			}
		})
	}
}

// TestWriteMalformedCSINoPanic feeds malformed CSI sequences through the public Write path and asserts they do not
// panic. Malformed CSI args (negative, overflow) reaching insertBlanks / deleteChars must be clamped, not trusted as
// slice bounds.
func TestWriteMalformedCSINoPanic(t *testing.T) {
	// 'P' = DCH (delete chars), '@' = ICH (insert chars).
	sequences := []string{
		"\x1b[-1@",
		"\x1b[-1P",
		"\x1b[0@",
		"\x1b[0P",
		fmt.Sprintf("\x1b[%d@", math.MaxInt),
		fmt.Sprintf("\x1b[%d@", math.MaxInt64),
		fmt.Sprintf("\x1b[%dP", math.MaxInt),
		fmt.Sprintf("\x1b[%dP", math.MaxInt64),
		// Position cursor near the right margin, then attempt large insert/delete.
		"\x1b[1;79H\x1b[999999@",
		"\x1b[1;79H\x1b[999999P",
	}

	for _, seq := range sequences {
		t.Run(fmt.Sprintf("%q", seq), func(t *testing.T) {
			vt := New()
			vt.Resize(80, 24)

			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Write(%q) panicked: %v", seq, r)
				}
			}()

			if _, err := vt.Write([]byte(seq)); err != nil {
				t.Fatalf("Write(%q) returned error: %v", seq, err)
			}
		})
	}
}
