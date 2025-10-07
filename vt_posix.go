// +build linux darwin dragonfly solaris openbsd netbsd freebsd

package vt10x

import (
	"bufio"
	"bytes"
	"io"
	"slices"
	"unicode"
	"unicode/utf8"
)

type terminal struct {
	*State
}

func newTerminal(info TerminalInfo) *terminal {
	t := &terminal{newState(info.w)}
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

func uniqueSorted(m map[int]bool) []int {
	lines := make([]int, 0, len(m))
	for line := range m {
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return lines
	}

	slices.Sort(lines)
	return lines
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
