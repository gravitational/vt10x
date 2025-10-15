package vt10x

import (
	"math/rand"
	"testing"
)

func TestDumpState(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*State)
		check func(*testing.T, TerminalState)
	}{
		{
			name: "default state",
			setup: func(s *State) {
				s.resize(80, 24)
				s.reset()
			},
			check: func(t *testing.T, state TerminalState) {
				if state.Cols != 80 {
					t.Errorf("expected 80 cols, got %d", state.Cols)
				}
				if state.Rows != 24 {
					t.Errorf("expected 24 rows, got %d", state.Rows)
				}
				if state.CursorX != 0 || state.CursorY != 0 {
					t.Errorf("expected cursor at (0,0), got (%d,%d)", state.CursorX, state.CursorY)
				}
				if !state.CursorVisible {
					t.Error("expected cursor to be visible")
				}
				if state.AltScreen {
					t.Error("expected primary screen")
				}
				if state.ScrollTop != 0 {
					t.Errorf("expected scroll top 0, got %d", state.ScrollTop)
				}
				if state.ScrollBottom != 23 {
					t.Errorf("expected scroll bottom 23, got %d", state.ScrollBottom)
				}
				if !state.Wrap {
					t.Error("expected wrap mode to be enabled")
				}
			},
		},
		{
			name: "cursor position",
			setup: func(s *State) {
				s.resize(80, 24)
				s.reset()
				s.moveTo(10, 5)
			},
			check: func(t *testing.T, state TerminalState) {
				if state.CursorX != 10 {
					t.Errorf("expected cursor X=10, got %d", state.CursorX)
				}
				if state.CursorY != 5 {
					t.Errorf("expected cursor Y=5, got %d", state.CursorY)
				}
			},
		},
		{
			name: "hidden cursor",
			setup: func(s *State) {
				s.resize(80, 24)
				s.reset()
				s.mode |= ModeHide
			},
			check: func(t *testing.T, state TerminalState) {
				if state.CursorVisible {
					t.Error("expected cursor to be hidden")
				}
			},
		},
		{
			name: "alternate screen",
			setup: func(s *State) {
				s.resize(80, 24)
				s.reset()
				s.swapScreen()
			},
			check: func(t *testing.T, state TerminalState) {
				if !state.AltScreen {
					t.Error("expected alternate screen")
				}
			},
		},
		{
			name: "scroll region",
			setup: func(s *State) {
				s.resize(80, 24)
				s.reset()
				s.setScroll(5, 15)
			},
			check: func(t *testing.T, state TerminalState) {
				if state.ScrollTop != 5 {
					t.Errorf("expected scroll top 5, got %d", state.ScrollTop)
				}
				if state.ScrollBottom != 15 {
					t.Errorf("expected scroll bottom 15, got %d", state.ScrollBottom)
				}
			},
		},
		{
			name: "tab stops",
			setup: func(s *State) {
				s.resize(80, 24)
				s.reset()
			},
			check: func(t *testing.T, state TerminalState) {
				// Default tab stops should be at every 8 columns
				expectedTabs := []int{8, 16, 24, 32, 40, 48, 56, 64, 72}
				if len(state.TabStops) != len(expectedTabs) {
					t.Errorf("expected %d tab stops, got %d", len(expectedTabs), len(state.TabStops))
				}
				for i, tab := range expectedTabs {
					if i < len(state.TabStops) && state.TabStops[i] != tab {
						t.Errorf("expected tab stop at %d, got %d", tab, state.TabStops[i])
					}
				}
			},
		},
		{
			name: "saved cursor",
			setup: func(s *State) {
				s.resize(80, 24)
				s.reset()
				s.moveTo(20, 10)
				s.saveCursor()
				s.moveTo(0, 0)
			},
			check: func(t *testing.T, state TerminalState) {
				if state.SavedCursorX != 20 {
					t.Errorf("expected saved cursor X=20, got %d", state.SavedCursorX)
				}
				if state.SavedCursorY != 10 {
					t.Errorf("expected saved cursor Y=10, got %d", state.SavedCursorY)
				}
				if state.CursorX != 0 || state.CursorY != 0 {
					t.Errorf("expected current cursor at (0,0), got (%d,%d)", state.CursorX, state.CursorY)
				}
			},
		},
		{
			name: "title",
			setup: func(s *State) {
				s.resize(80, 24)
				s.reset()
				s.setTitle("Test Terminal")
			},
			check: func(t *testing.T, state TerminalState) {
				if state.Title != "Test Terminal" {
					t.Errorf("expected title 'Test Terminal', got '%s'", state.Title)
				}
			},
		},
		{
			name: "mode flags",
			setup: func(s *State) {
				s.resize(80, 24)
				s.reset()
				s.mode |= ModeInsert | ModeReverse
				s.mode &^= ModeWrap
				s.cur.State |= cursorOrigin
			},
			check: func(t *testing.T, state TerminalState) {
				if !state.Insert {
					t.Error("expected insert mode")
				}
				if !state.ReverseVideo {
					t.Error("expected reverse video")
				}
				if state.Wrap {
					t.Error("expected wrap mode to be disabled")
				}
				if !state.Origin {
					t.Error("expected origin mode")
				}
			},
		},
		{
			name: "buffer content",
			setup: func(s *State) {
				s.resize(10, 5)
				s.reset()
				// Add some content to primary buffer
				s.setChar('A', &s.cur.Attr, 0, 0)
				s.setChar('B', &s.cur.Attr, 1, 0)
				s.setChar('C', &s.cur.Attr, 0, 1)
				// Switch to alt screen and add content
				s.swapScreen()
				s.setChar('X', &s.cur.Attr, 2, 2)
				s.setChar('Y', &s.cur.Attr, 3, 2)
			},
			check: func(t *testing.T, state TerminalState) {
				if len(state.PrimaryBuffer) != 5 {
					t.Errorf("expected 5 rows in primary buffer, got %d", len(state.PrimaryBuffer))
				}
				if len(state.AlternateBuffer) != 5 {
					t.Errorf("expected 5 rows in alternate buffer, got %d", len(state.AlternateBuffer))
				}
				if !state.AltScreen {
					t.Error("expected to be on alternate screen")
				}
				if state.PrimaryBuffer[2][2].Char != 'X' {
					t.Errorf("expected 'X' at primary[2][2], got '%c'", state.PrimaryBuffer[2][2].Char)
				}
				if state.PrimaryBuffer[2][3].Char != 'Y' {
					t.Errorf("expected 'Y' at primary[2][3], got '%c'", state.PrimaryBuffer[2][3].Char)
				}
				if state.AlternateBuffer[0][0].Char != 'A' {
					t.Errorf("expected 'A' at alternate[0][0], got '%c'", state.AlternateBuffer[0][0].Char)
				}
				if state.AlternateBuffer[0][1].Char != 'B' {
					t.Errorf("expected 'B' at alternate[0][1], got '%c'", state.AlternateBuffer[0][1].Char)
				}
				if state.AlternateBuffer[1][0].Char != 'C' {
					t.Errorf("expected 'C' at alternate[1][0], got '%c'", state.AlternateBuffer[1][0].Char)
				}
			},
		},
		{
			name: "glyph attributes",
			setup: func(s *State) {
				s.resize(10, 5)
				s.reset()

				s.cur.Attr.FG = Color(1)
				s.cur.Attr.BG = Color(2)
				s.cur.Attr.Mode = attrBold | attrUnderline
				s.setChar('T', &s.cur.Attr, 0, 0)
			},
			check: func(t *testing.T, state TerminalState) {
				glyph := state.PrimaryBuffer[0][0]
				if glyph.Char != 'T' {
					t.Errorf("expected 'T', got '%c'", glyph.Char)
				}
				if glyph.FG != Color(9) { // Bold makes FG color bright (1 + 8)
					t.Errorf("expected FG color 9, got %d", glyph.FG)
				}
				if glyph.BG != Color(2) {
					t.Errorf("expected BG color 2, got %d", glyph.BG)
				}
				if glyph.Mode&attrBold == 0 {
					t.Error("expected bold attribute")
				}
				if glyph.Mode&attrUnderline == 0 {
					t.Error("expected underline attribute")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newState(nil)
			tt.setup(s)
			state := s.DumpState()
			tt.check(t, state)
		})
	}
}

func TestDumpStateBufferSizes(t *testing.T) {
	s := newState(nil)
	s.resize(100, 50)
	s.reset()

	state := s.DumpState()

	if len(state.PrimaryBuffer) != 50 {
		t.Errorf("expected 50 rows in primary buffer, got %d", len(state.PrimaryBuffer))
	}
	if len(state.AlternateBuffer) != 50 {
		t.Errorf("expected 50 rows in alternate buffer, got %d", len(state.AlternateBuffer))
	}

	for i, row := range state.PrimaryBuffer {
		if len(row) != 100 {
			t.Errorf("expected 100 cols in primary buffer row %d, got %d", i, len(row))
		}
	}
	for i, row := range state.AlternateBuffer {
		if len(row) != 100 {
			t.Errorf("expected 100 cols in alternate buffer row %d, got %d", i, len(row))
		}
	}
}

func TestDumpStateEmptyTerminal(t *testing.T) {
	s := newState(nil)
	state := s.DumpState()

	if state.Cols != 0 || state.Rows != 0 {
		t.Errorf("expected 0x0 terminal, got %dx%d", state.Cols, state.Rows)
	}
	if len(state.PrimaryBuffer) != 0 {
		t.Errorf("expected empty primary buffer, got %d rows", len(state.PrimaryBuffer))
	}
	if len(state.AlternateBuffer) != 0 {
		t.Errorf("expected empty alternate buffer, got %d rows", len(state.AlternateBuffer))
	}
}

func generateSrc(t *State) []line {
	src := make([]line, t.rows)
	for y := 0; y < t.rows; y++ {
		src[y] = make([]Glyph, t.cols)
		for x := 0; x < t.cols; x++ {
			src[y][x] = Glyph{Char: rune(rand.Intn(128))}
		}
	}
	return src
}

func BenchmarkCopyBufferOriginal(b *testing.B) {
	t := State{
		rows: 270,
		cols: 62,
	}

	src := generateSrc(&t)
	copyBuffer := func(src []line) [][]Glyph {
		buf := make([][]Glyph, t.rows)
		for y := 0; y < t.rows; y++ {
			buf[y] = make([]Glyph, t.cols)
			for x := 0; x < t.cols; x++ {
				if y < len(src) && x < len(src[y]) {
					buf[y][x] = src[y][x]
				}
			}
		}
		return buf
	}

	b.ResetTimer()
	for b.Loop() {
		_ = copyBuffer(src)
	}
}

func BenchmarkCopyBufferFlat(b *testing.B) {
	t := State{
		rows: 270,
		cols: 62,
	}

	src := generateSrc(&t)

	copyBuffer := func(src []line) [][]Glyph {
		buf := make([][]Glyph, t.rows)
		flat := make([]Glyph, t.rows*t.cols)

		for y := 0; y < t.rows; y++ {
			row := flat[y*t.cols : (y+1)*t.cols]
			buf[y] = row
			if y < len(src) {
				copy(row, src[y])
			}
		}

		return buf
	}

	for b.Loop() {
		_ = copyBuffer(src)
	}
}
