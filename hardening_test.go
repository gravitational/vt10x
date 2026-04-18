package vt10x

import (
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
	}

	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		vt := New(WithSize(80, 24))
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
