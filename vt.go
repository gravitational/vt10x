package vt10x

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"maps"
	"slices"
	"unicode"
	"unicode/utf8"
)

// Terminal represents the virtual terminal emulator.
type Terminal interface {
	// View displays the virtual terminal.
	View

	// Write parses input and writes terminal changes to state.
	io.Writer

	// Parse blocks on read on pty or io.Reader, then parses sequences until
	// buffer empties. State is locked as soon as first rune is read, and unlocked
	// when buffer is empty.
	Parse(bf *bufio.Reader) error

	// WriteWithChanges writes terminal changes to state and returns the line numbers that changed.
	WriteWithChanges(p []byte) ([]int, error)

	// TakeScrollback returns the text of lines that have scrolled off the top since the last call, along with the
	// number of additional scrolled-off lines that were dropped because the capture limit was reached, then resets
	// both. Only primary-screen lines that scroll off the top row of the screen are recorded: alternate-screen
	// scrolls, deleted lines, and scrolls of a region that does not start at the top row are not. It returns
	// nothing unless capture was enabled with WithScrollbackCapture.
	TakeScrollback() (lines [][]rune, dropped int)
}

// View represents the view of the virtual terminal emulator.
type View interface {
	// String dumps the virtual terminal contents.
	fmt.Stringer

	// Size returns the size of the virtual terminal.
	Size() (cols, rows int)

	// Resize changes the size of the virtual terminal.
	Resize(cols, rows int)

	// Mode returns the current terminal mode.//
	Mode() ModeFlag

	// Title represents the title of the console window.
	Title() string

	// Cell returns the glyph containing the character code, foreground color, and
	// background color at position (x, y) relative to the top left of the terminal.
	Cell(x, y int) Glyph

	// Cursor returns the current position of the cursor.
	Cursor() Cursor

	// CursorVisible returns the visible state of the cursor.
	CursorVisible() bool

	// Lock locks the state object's mutex.
	Lock()

	// Unlock resets change flags and unlocks the state object's mutex.
	Unlock()

	// DumpState returns the current state of the terminal.
	DumpState() TerminalState
}

type TerminalOption func(*TerminalInfo)

type TerminalInfo struct {
	w               io.Writer
	cols, rows      int
	scrollbackLimit int
}

func WithWriter(w io.Writer) TerminalOption {
	return func(info *TerminalInfo) {
		if w == nil {
			return
		}
		info.w = w
	}
}

func WithSize(cols, rows int) TerminalOption {
	return func(info *TerminalInfo) {
		info.cols = cols
		info.rows = rows
	}
}

// WithScrollbackCapture enables capturing the text of lines as they scroll off the top of the screen, retrievable
// via TakeScrollback. limit caps the number of lines retained between calls (excess is counted as dropped) so an
// unbounded scroll cannot exhaust memory. A non-positive limit disables capture (the default).
func WithScrollbackCapture(limit int) TerminalOption {
	return func(info *TerminalInfo) {
		if limit < 0 {
			limit = 0
		}
		info.scrollbackLimit = limit
	}
}

// New returns a new virtual terminal emulator.
func New(opts ...TerminalOption) Terminal {
	info := TerminalInfo{
		w:    ioutil.Discard,
		cols: 80,
		rows: 24,
	}
	for _, opt := range opts {
		opt(&info)
	}
	return newTerminal(info)
}

type terminal struct {
	*State
}

func newTerminal(info TerminalInfo) *terminal {
	t := &terminal{newState(info.w)}
	t.scrollbackLimit = info.scrollbackLimit
	t.init(info.cols, info.rows)
	return t
}

func (t *terminal) init(cols, rows int) {
	t.numlock = true
	t.state = t.parse
	t.cur.Attr.FG = DefaultFG
	t.cur.Attr.BG = DefaultBG
	t.Resize(cols, rows)
	t.reset()
}

// Write parses input and writes terminal changes to state.
func (t *terminal) Write(p []byte) (int, error) {
	var written int
	r := bytes.NewReader(p)
	t.lock()
	defer t.unlock()
	for {
		c, sz, err := r.ReadRune()
		if err != nil {
			if err == io.EOF {
				break
			}
			return written, err
		}
		written += sz
		if c == unicode.ReplacementChar && sz == 1 {
			if r.Len() == 0 {
				// not enough bytes for a full rune
				return written - 1, nil
			}
			t.logln("invalid utf8 sequence")
			continue
		}
		t.put(c)
	}
	return written, nil
}

// WriteWithChanges writes to the terminal state and returns the line numbers that changed.
func (t *terminal) WriteWithChanges(p []byte) ([]int, error) {
	var dirtyLines = make(map[int]bool)
	r := bytes.NewReader(p)
	t.lock()

	prevRow := t.cur.Y

	defer t.unlock()
	for {
		c, sz, err := r.ReadRune()
		if err != nil {
			if err == io.EOF {
				break
			}
			return uniqueSorted(dirtyLines), err
		}
		if c == unicode.ReplacementChar && sz == 1 {
			if r.Len() == 0 {
				return uniqueSorted(dirtyLines), nil
			}
			t.logln("invalid utf8 sequence")
			continue
		}

		beforeRow := t.cur.Y
		t.put(c)
		afterRow := t.cur.Y

		dirtyLines[beforeRow] = true
		if afterRow != beforeRow {
			dirtyLines[afterRow] = true
		}

		if t.cur.Y != prevRow {
			prevRow = t.cur.Y
		}
	}

	return uniqueSorted(dirtyLines), nil
}

// TODO: add tests for expected blocking behavior
func (t *terminal) Parse(br *bufio.Reader) error {
	var locked bool
	defer func() {
		if locked {
			t.unlock()
		}
	}()
	for {
		c, sz, err := br.ReadRune()
		if err != nil {
			return err
		}
		if c == unicode.ReplacementChar && sz == 1 {
			t.logln("invalid utf8 sequence")
			break
		}
		if !locked {
			t.lock()
			locked = true
		}

		// put rune for parsing and update state
		t.put(c)

		// break if our buffer is empty, or if buffer contains an
		// incomplete rune.
		n := br.Buffered()
		if n == 0 || (n < 4 && !fullRuneBuffered(br)) {
			break
		}
	}
	return nil
}

func fullRuneBuffered(br *bufio.Reader) bool {
	n := br.Buffered()
	buf, err := br.Peek(n)
	if err != nil {
		return false
	}
	return utf8.FullRune(buf)
}

func (t *terminal) Resize(cols, rows int) {
	t.lock()
	defer t.unlock()
	_ = t.resize(cols, rows)
}

func uniqueSorted(m map[int]bool) []int {
	return slices.Sorted(maps.Keys(m))
}
