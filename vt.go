package vt10x

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
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
