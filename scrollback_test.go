package vt10x

import (
	"strings"
	"testing"
)

func TestTakeScrollbackDisabledByDefault(t *testing.T) {
	term := New(WithSize(10, 3))

	writeLines(t, term, "l0", "l1", "l2", "l3", "l4")
	assertScrollback(t, term, nil, 0)

	// Capture is off, but the writes still scrolled the screen.
	if s := extractStr(term, 0, 1, 0); s != "l2" {
		t.Errorf("expected screen row 0 to be l2, got %q", s)
	}
}

func TestWithScrollbackCaptureNonPositiveLimit(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit int
	}{
		{name: "zero limit", limit: 0},
		{name: "negative limit", limit: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			term := New(WithSize(10, 3), WithScrollbackCapture(tc.limit))

			writeLines(t, term, "l0", "l1", "l2", "l3", "l4")

			lines, dropped := term.TakeScrollback()
			if lines != nil || dropped != 0 {
				t.Errorf("expected no capture, got %d lines, %d dropped", len(lines), dropped)
			}

			// Capture is off, but the writes still scrolled the screen.
			if s := extractStr(term, 0, 1, 0); s != "l2" {
				t.Errorf("expected screen row 0 to be l2, got %q", s)
			}
		})
	}
}

func TestScrollbackCapturesScrolledOffLines(t *testing.T) {
	term := New(WithSize(10, 3), WithScrollbackCapture(100))

	// 5 lines on a 3-row screen: l0 and l1 scroll off the top.
	writeLines(t, term, "l0", "héllo", "l2", "l3", "l4")

	lines, dropped := term.TakeScrollback()
	got := scrollbackStrings(lines)
	if len(got) != 2 || got[0] != "l0" || got[1] != "héllo" {
		t.Fatalf("expected [l0 héllo], got %q", got)
	}

	if dropped != 0 {
		t.Errorf("expected 0 dropped, got %d", dropped)
	}

	// Captured lines are full screen-width rows padded with blanks.
	for i, line := range lines {
		if len(line) != 10 {
			t.Errorf("line %d: expected width 10, got %d", i, len(line))
		}
	}

	// The screen itself shows the remaining lines.
	if s := extractStr(term, 0, 1, 0); s != "l2" {
		t.Errorf("expected screen row 0 to be l2, got %q", s)
	}
}

func TestTakeScrollbackDrainsAndResets(t *testing.T) {
	term := New(WithSize(10, 3), WithScrollbackCapture(100))

	writeLines(t, term, "l0", "l1", "l2", "l3", "l4")
	assertScrollback(t, term, []string{"l0", "l1"}, 0)

	// Drained: a second take returns nothing.
	assertScrollback(t, term, nil, 0)

	// Capture resumes after draining.
	writeLines(t, term, "", "l5")
	assertScrollback(t, term, []string{"l2"}, 0)
}

func TestScrollbackLimitDropsExcess(t *testing.T) {
	term := New(WithSize(10, 3), WithScrollbackCapture(3))

	// 10 lines on a 3-row screen: 7 scroll off, only 3 retained.
	writeLines(t, term, "l0", "l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8", "l9")
	assertScrollback(t, term, []string{"l0", "l1", "l2"}, 4)

	// Taking resets the limit budget as well as the dropped count.
	writeLines(t, term, "", "l10", "l11")
	assertScrollback(t, term, []string{"l7", "l8"}, 0)
}

func TestScrollbackMultiLineScrollStraddlingLimit(t *testing.T) {
	term := New(WithSize(10, 5), WithScrollbackCapture(2))

	writeLines(t, term, "l0", "l1", "l2", "l3", "l4")

	// Scroll the whole 5-row screen up at once: 2 captured, 3 dropped from within the single scroll event.
	if _, err := term.Write([]byte("\033[5S")); err != nil {
		t.Fatalf("write: %v", err)
	}

	assertScrollback(t, term, []string{"l0", "l1"}, 3)
}

func TestScrollbackCSIScrollUp(t *testing.T) {
	term := New(WithSize(10, 4), WithScrollbackCapture(10))

	writeLines(t, term, "l0", "l1", "l2", "l3")

	if _, err := term.Write([]byte("\033[2S")); err != nil {
		t.Fatalf("write: %v", err)
	}
	assertScrollback(t, term, []string{"l0", "l1"}, 0)

	if s := extractStr(term, 0, 1, 0); s != "l2" {
		t.Errorf("expected screen row 0 to be l2, got %q", s)
	}
}

func TestScrollbackNotCapturedOnScrollDown(t *testing.T) {
	term := New(WithSize(10, 4), WithScrollbackCapture(10))

	writeLines(t, term, "l0", "l1", "l2", "l3")

	// Scroll down (CSI T) and reverse index at the top of the screen (ESC M) push lines off the bottom, not into scrollback.
	if _, err := term.Write([]byte("\033[2T\033[H\033M")); err != nil {
		t.Fatalf("write: %v", err)
	}

	assertScrollback(t, term, nil, 0)

	// The scrolls did happen: l0 moved down two rows from CSI T plus one more from ESC M.
	if s := extractStr(term, 0, 1, 3); s != "l0" {
		t.Errorf("expected screen row 3 to be l0, got %q", s)
	}
}

func TestScrollbackCaptureOnShrinkResize(t *testing.T) {
	term := New(WithSize(10, 5), WithScrollbackCapture(100))

	writeLines(t, term, "l0", "l1", "l2", "l3", "l4")

	// Shrinking to 2 rows with the cursor on the last row slides the buffer up by 3; the discarded top rows land in scrollback.
	term.Resize(10, 2)
	assertScrollback(t, term, []string{"l0", "l1", "l2"}, 0)

	if s := extractStr(term, 0, 1, 0); s != "l3" {
		t.Errorf("expected screen row 0 to be l3, got %q", s)
	}
}

func TestScrollbackNoCaptureOnShrinkResizeWithCursorHigh(t *testing.T) {
	term := New(WithSize(10, 5), WithScrollbackCapture(100))

	if _, err := term.Write([]byte("l0")); err != nil {
		t.Fatalf("write: %v", err)
	}

	term.Resize(10, 2)
	assertScrollback(t, term, nil, 0)

	if s := extractStr(term, 0, 1, 0); s != "l0" {
		t.Errorf("expected screen row 0 to be l0, got %q", s)
	}
}

func TestScrollbackNotCapturedOnInteriorRegionScroll(t *testing.T) {
	term := New(WithSize(10, 5), WithScrollbackCapture(10))

	writeLines(t, term, "l0", "l1", "l2", "l3", "l4")

	// Scrolling a 2..4 region (top=1) discards l1 and l2 inside the screen, not off the top of it.
	if _, err := term.Write([]byte("\033[2;4r\033[2S")); err != nil {
		t.Fatalf("write: %v", err)
	}

	assertScrollback(t, term, nil, 0)

	// The region scroll itself happened: row 0 stayed put and l3 moved to the region top.
	if s := extractStr(term, 0, 1, 0); s != "l0" {
		t.Errorf("expected screen row 0 to be l0, got %q", s)
	}

	if s := extractStr(term, 0, 1, 1); s != "l3" {
		t.Errorf("expected screen row 1 to be l3, got %q", s)
	}
}

func TestScrollbackCapturedOnTopRegionScroll(t *testing.T) {
	term := New(WithSize(10, 5), WithScrollbackCapture(10))

	writeLines(t, term, "l0", "l1", "l2", "l3", "l4")

	// A region pinned to row 0 still scrolls lines off the top of the screen.
	if _, err := term.Write([]byte("\033[1;3r\033[2S")); err != nil {
		t.Fatalf("write: %v", err)
	}

	assertScrollback(t, term, []string{"l0", "l1"}, 0)

	// Rows below the region stayed put.
	if s := extractStr(term, 0, 1, 3); s != "l3" {
		t.Errorf("expected screen row 3 to be l3, got %q", s)
	}
}

func TestScrollbackNotCapturedOnDeleteLines(t *testing.T) {
	term := New(WithSize(10, 5), WithScrollbackCapture(10))

	writeLines(t, term, "l0", "l1", "l2", "l3", "l4")

	// CSI M deletes lines at the cursor; they never scrolled off the top of the screen.
	if _, err := term.Write([]byte("\033[3;1H\033[2M")); err != nil {
		t.Fatalf("write: %v", err)
	}

	assertScrollback(t, term, nil, 0)

	// The deletion itself happened: l4 moved up to row 2.
	if s := extractStr(term, 0, 1, 2); s != "l4" {
		t.Errorf("expected screen row 2 to be l4, got %q", s)
	}
}

func TestScrollbackNotCapturedOnAltScreen(t *testing.T) {
	term := New(WithSize(10, 3), WithScrollbackCapture(10))

	writeLines(t, term, "l0")

	if _, err := term.Write([]byte("\033[?1049h\033[H")); err != nil {
		t.Fatalf("write: %v", err)
	}

	if term.(*terminal).mode&ModeAltScreen == 0 {
		t.Fatal("alt screen not active")
	}

	// Scrolling on the alternate screen is not primary-screen history.
	writeLines(t, term, "a0", "a1", "a2", "a3", "a4")

	if _, err := term.Write([]byte("\033[?1049l")); err != nil {
		t.Fatalf("write: %v", err)
	}

	assertScrollback(t, term, nil, 0)

	// The primary screen content is restored untouched.
	if s := extractStr(term, 0, 1, 0); s != "l0" {
		t.Errorf("expected screen row 0 to be l0, got %q", s)
	}
}

func TestScrollbackShrinkResizeOnAltScreenCapturesPrimary(t *testing.T) {
	term := New(WithSize(10, 5), WithScrollbackCapture(10))

	writeLines(t, term, "l0", "l1", "l2", "l3", "l4")

	// Fill the alt screen so its cursor sits on the bottom row.
	if _, err := term.Write([]byte("\033[?1049h\033[H")); err != nil {
		t.Fatalf("write: %v", err)
	}

	writeLines(t, term, "a0", "a1", "a2", "a3", "a4")

	// The shrink slides both buffers up by 3; the history lost is the primary screen's rows, not the alt screen's.
	term.Resize(10, 2)
	assertScrollback(t, term, []string{"l0", "l1", "l2"}, 0)
}

func TestScrollbackOversizedScrollClamped(t *testing.T) {
	term := New(WithSize(10, 4), WithScrollbackCapture(2))

	writeLines(t, term, "l0", "l1", "l2", "l3")

	// A scroll count far past the screen height is clamped before capture, so the dropped count reflects
	// the 4 real rows (2 captured, 2 dropped), not the raw argument.
	if _, err := term.Write([]byte("\033[99S")); err != nil {
		t.Fatalf("write: %v", err)
	}

	assertScrollback(t, term, []string{"l0", "l1"}, 2)
}

func TestCaptureScrollbackPastBufferEnd(t *testing.T) {
	s := newState(nil)
	s.resize(10, 3)
	s.reset()
	s.scrollbackLimit = 10
	s.setChar('a', &s.cur.Attr, 0, 0)

	// A count past the end of the buffer captures only the rows that exist; rows past the end are not
	// counted as dropped either.
	s.captureScrollback(s.lines, 5)

	if len(s.scrollback) != 3 {
		t.Fatalf("expected 3 captured lines, got %d", len(s.scrollback))
	}

	if s.scrollback[0][0] != 'a' {
		t.Errorf("expected first captured line to start with 'a', got %q", s.scrollback[0][0])
	}

	if s.scrollbackDropped != 0 {
		t.Errorf("expected 0 dropped, got %d", s.scrollbackDropped)
	}
}

// scrollbackStrings converts captured scrollback lines to strings with trailing blank cells removed.
func scrollbackStrings(lines [][]rune) []string {
	out := make([]string, len(lines))

	for i, line := range lines {
		out[i] = strings.TrimRight(string(line), " ")
	}

	return out
}

func writeLines(t *testing.T, term Terminal, lines ...string) {
	t.Helper()

	if _, err := term.Write([]byte(strings.Join(lines, "\r\n"))); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func assertScrollback(t *testing.T, term Terminal, wantLines []string, wantDropped int) {
	t.Helper()

	lines, dropped := term.TakeScrollback()
	got := scrollbackStrings(lines)
	if len(got) != len(wantLines) {
		t.Fatalf("expected %d scrollback lines, got %d: %q", len(wantLines), len(got), got)
	}

	for i := range wantLines {
		if got[i] != wantLines[i] {
			t.Errorf("scrollback line %d: expected %q, got %q", i, wantLines[i], got[i])
		}
	}

	if dropped != wantDropped {
		t.Errorf("expected %d dropped lines, got %d", wantDropped, dropped)
	}
}
