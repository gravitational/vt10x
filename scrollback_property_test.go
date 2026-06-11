package vt10x

import (
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// rapidAlphabet deliberately excludes space so written text never has trailing blanks, which keeps
// TrimRight-based comparisons exact. Multi-byte runes are included since the emulator stores one rune per cell.
const rapidAlphabet = "abcdefgh01234#_あ⌘"

// capturedLine is a model scrollback entry: the trimmed text and the screen width at capture time, which the
// emulator's captured lines keep even if the screen is later resized.
type capturedLine struct {
	text  string
	width int
}

// scrollbackModel is a pure-Go reference model of the screen and scrollback semantics for the operations driven
// by TestScrollbackStateMachine. The op set only ever writes lines ending in \r\n (wrapping included), so the
// cursor always sits in column 0 and every row at or below the cursor is blank.
type scrollbackModel struct {
	cols, rows int
	limit      int
	cursorRow  int
	screen     []string // trimmed text of each row
	scrollback []capturedLine
	dropped    int
}

func (m *scrollbackModel) push(text string) {
	if m.limit <= 0 {
		return
	}

	if len(m.scrollback) >= m.limit {
		m.dropped++
		return
	}

	m.scrollback = append(m.scrollback, capturedLine{text: text, width: m.cols})
}

func (m *scrollbackModel) writeLine(text string) {
	m.screen[m.cursorRow] = text
	if m.cursorRow < m.rows-1 {
		m.cursorRow++
		return
	}

	// Newline at the bottom: the top row scrolls off into scrollback.
	m.push(m.screen[0])
	copy(m.screen, m.screen[1:])
	m.screen[m.rows-1] = ""
}

func (m *scrollbackModel) scrollUp(n int) {
	if n > m.rows {
		n = m.rows
	}

	for y := 0; y < n; y++ {
		m.push(m.screen[y])
	}

	copy(m.screen, m.screen[n:])
	for y := m.rows - n; y < m.rows; y++ {
		m.screen[y] = ""
	}
}

// resize applies a newCols x newRows resize. slideCursor is the active screen's cursor row, which determines how
// far the buffers slide up when shrinking; the slid-off rows are captured at their pre-resize width.
func (m *scrollbackModel) resize(newCols, newRows, slideCursor int) {
	slide := slideCursor - newRows + 1
	if slide > 0 {
		for y := 0; y < slide; y++ {
			m.push(m.screen[y])
		}
		m.screen = m.screen[slide:]
	}

	for len(m.screen) < newRows {
		m.screen = append(m.screen, "")
	}
	m.screen = m.screen[:newRows]

	if newCols < m.cols {
		for y, row := range m.screen {
			if runes := []rune(row); len(runes) > newCols {
				m.screen[y] = string(runes[:newCols])
			}
		}
	}

	m.cols, m.rows = newCols, newRows
	if m.cursorRow > newRows-1 {
		m.cursorRow = newRows - 1
	}
}

// TestScrollbackStateMachine drives random interleavings of writes, scrolls, deletes, alt-screen excursions,
// row-resizes, and TakeScrollback drains against a reference model, checking the screen, cursor, and scrollback
// stay in lockstep after every operation.
func TestScrollbackStateMachine(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		cols := rapid.IntRange(2, 16).Draw(rt, "cols")
		rows := rapid.IntRange(2, 8).Draw(rt, "rows")
		limit := rapid.IntRange(0, 5).Draw(rt, "limit")

		term := New(WithSize(cols, rows), WithScrollbackCapture(limit))
		m := &scrollbackModel{
			cols:   cols,
			rows:   rows,
			limit:  limit,
			screen: make([]string, rows),
		}
		// lineText generates at most one screen row of text at the current width, so it never wraps.
		lineText := func(rt *rapid.T, label string) string {
			return rapid.StringOfN(rapid.RuneFrom([]rune(rapidAlphabet)), 0, m.cols, -1).Draw(rt, label)
		}

		write := func(s string) {
			if _, err := term.Write([]byte(s)); err != nil {
				rt.Fatalf("write %q: %v", s, err)
			}
		}

		rt.Repeat(map[string]func(*rapid.T){
			"writeLine": func(rt *rapid.T) {
				// Up to three screen widths: anything beyond cols wraps, and a wrap at the bottom row
				// scrolls and captures exactly like a newline, so the model consumes one row per chunk.
				text := rapid.StringOfN(rapid.RuneFrom([]rune(rapidAlphabet)), 0, 3*m.cols, -1).Draw(rt, "text")
				write(text + "\r\n")
				for runes := []rune(text); ; runes = runes[m.cols:] {
					m.writeLine(string(runes[:min(len(runes), m.cols)]))
					if len(runes) <= m.cols {
						break
					}
				}
			},
			"scrollUp": func(rt *rapid.T) {
				n := rapid.IntRange(1, m.rows+2).Draw(rt, "n")
				write(fmt.Sprintf("\033[%dS", n))
				m.scrollUp(n)
			},
			"index": func(rt *rapid.T) {
				write("\033D")
				if m.cursorRow < m.rows-1 {
					m.cursorRow++
					return
				}
				m.push(m.screen[0])
				copy(m.screen, m.screen[1:])
				m.screen[m.rows-1] = ""
			},
			"deleteLines": func(rt *rapid.T) {
				n := rapid.IntRange(1, m.rows+2).Draw(rt, "n")
				write(fmt.Sprintf("\033[%dM", n))
				// No model change: every row at or below the cursor is blank under this op set, so the
				// deletion moves nothing. This op only guards that deleted lines never reach scrollback
				// (count-wise); DL's actual line movement is pinned by TestScrollbackNotCapturedOnDeleteLines.
			},
			"altExcursion": func(rt *rapid.T) {
				var sb strings.Builder
				sb.WriteString("\033[?1049h\033[H")
				for i, k := 0, rapid.IntRange(1, m.rows+2).Draw(rt, "altLines"); i < k; i++ {
					sb.WriteString(lineText(rt, "altText") + "\r\n")
				}
				sb.WriteString("\033[?1049l")
				write(sb.String())
				// No model change: alternate-screen activity never reaches the primary screen or scrollback.
				// Enter/exit always come as a balanced pair, so the machine never sits in alt mode at an op
				// boundary; unbalanced resets are exercised by strayAltReset and altResizeExcursion below.
			},
			"strayAltReset": func(rt *rapid.T) {
				// A defensive alt-screen reset while already on the primary screen must be a complete no-op
				// (1047 rather than 1049 so no saved-cursor restore is involved).
				write("\033[?1047l")
			},
			"altResizeExcursion": func(rt *rapid.T) {
				// Resize while the alt screen is active: the slide must capture the primary screen's rows.
				// The alt cursor is kept at least as deep as the primary cursor so the slide never truncates
				// primary content below the restored cursor, which would break the blank-below-cursor invariant.
				k := rapid.IntRange(m.cursorRow, m.rows+2).Draw(rt, "altLines")
				newCols := rapid.IntRange(1, 16).Draw(rt, "newCols")
				newRows := rapid.IntRange(1, 10).Draw(rt, "newRows")

				var sb strings.Builder
				sb.WriteString("\033[?1049h\033[H")
				for i := 0; i < k; i++ {
					sb.WriteString(lineText(rt, "altText") + "\r\n")
				}
				write(sb.String())
				term.Resize(newCols, newRows)
				write("\033[?1049l")

				if newCols != m.cols || newRows != m.rows {
					m.resize(newCols, newRows, min(k, m.rows-1))
				}
			},
			"resize": func(rt *rapid.T) {
				newCols := rapid.IntRange(1, 16).Draw(rt, "newCols")
				newRows := rapid.IntRange(1, 10).Draw(rt, "newRows")
				term.Resize(newCols, newRows)
				if newCols != m.cols || newRows != m.rows {
					m.resize(newCols, newRows, m.cursorRow)
				}
			},
			"rejectedResize": func(rt *rapid.T) {
				// Dimensions outside [1, maxResizeDim] are rejected outright and must change nothing.
				bad := rapid.SampledFrom([]int{0, -3, maxResizeDim + 1}).Draw(rt, "bad")
				if rapid.Bool().Draw(rt, "badCols") {
					term.Resize(bad, m.rows)
				} else {
					term.Resize(m.cols, bad)
				}
			},
			"take": func(rt *rapid.T) {
				lines, dropped := term.TakeScrollback()
				if len(lines) != len(m.scrollback) {
					rt.Fatalf("expected %d scrollback lines, got %d", len(m.scrollback), len(lines))
				}
				for i := range lines {
					if len(lines[i]) != m.scrollback[i].width {
						rt.Fatalf("scrollback line %d: expected width %d, got %d", i, m.scrollback[i].width, len(lines[i]))
					}
					if got := strings.TrimRight(string(lines[i]), " "); got != m.scrollback[i].text {
						rt.Fatalf("scrollback line %d: expected %q, got %q", i, m.scrollback[i].text, got)
					}
				}
				if dropped != m.dropped {
					rt.Fatalf("expected %d dropped, got %d", m.dropped, dropped)
				}
				m.scrollback, m.dropped = nil, 0
			},
			"": func(rt *rapid.T) {
				cur := term.Cursor()
				if cur.X != 0 || cur.Y != m.cursorRow {
					rt.Fatalf("cursor at (%d,%d), model expects (0,%d)", cur.X, cur.Y, m.cursorRow)
				}
				for y := 0; y < m.rows; y++ {
					if got := strings.TrimRight(extractStr(term, 0, m.cols-1, y), " "); got != m.screen[y] {
						rt.Fatalf("screen row %d: expected %q, got %q", y, m.screen[y], got)
					}
				}
				st := term.(*terminal)
				if len(st.scrollback) != len(m.scrollback) || st.scrollbackDropped != m.dropped {
					rt.Fatalf("pending scrollback %d lines/%d dropped, model expects %d/%d",
						len(st.scrollback), st.scrollbackDropped, len(m.scrollback), m.dropped)
				}
			},
		})
	})
}

// TestCaptureScrollbackAccountingProperty checks that for any grid, limit, and row count — including counts past
// the end of the buffer — captureScrollback retains exactly min(visible, limit) rows verbatim and counts the rest
// of the visible rows as dropped.
func TestCaptureScrollbackAccountingProperty(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		cols := rapid.IntRange(1, 12).Draw(rt, "cols")
		rows := rapid.IntRange(1, 8).Draw(rt, "rows")
		limit := rapid.IntRange(0, 10).Draw(rt, "limit")
		n := rapid.IntRange(0, rows+5).Draw(rt, "n")

		s := newState(nil)
		s.resize(cols, rows)
		s.reset()
		s.scrollbackLimit = limit

		// Start from a partially used budget: pre-existing retained lines and dropped count from earlier
		// captures since the last TakeScrollback.
		pre := 0
		if limit > 0 {
			pre = rapid.IntRange(0, limit).Draw(rt, "pre")
		}
		for i := 0; i < pre; i++ {
			s.scrollback = append(s.scrollback, []rune{'x'})
		}
		preDropped := rapid.IntRange(0, 3).Draw(rt, "preDropped")
		s.scrollbackDropped = preDropped

		runeGen := rapid.RuneFrom([]rune(rapidAlphabet))
		grid := make([]string, rows)
		for y := 0; y < rows; y++ {
			runes := make([]rune, cols)
			for x := range runes {
				runes[x] = runeGen.Draw(rt, "ch")
				s.setChar(runes[x], &s.cur.Attr, x, y)
			}
			grid[y] = string(runes)
		}

		s.captureScrollback(s.lines, n)

		visible := min(n, rows)
		captured := 0
		dropped := 0
		if limit > 0 {
			captured = min(visible, limit-pre)
			dropped = visible - captured
		}

		if len(s.scrollback) != pre+captured {
			rt.Fatalf("expected %d retained lines, got %d", pre+captured, len(s.scrollback))
		}
		for y := 0; y < captured; y++ {
			if got := string(s.scrollback[pre+y]); got != grid[y] {
				rt.Fatalf("captured line %d: expected %q, got %q", y, grid[y], got)
			}
		}
		if s.scrollbackDropped != preDropped+dropped {
			rt.Fatalf("expected %d dropped, got %d", preDropped+dropped, s.scrollbackDropped)
		}
	})
}
