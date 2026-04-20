package vt10x

import (
	"fmt"
	"runtime/debug"
)

// recoverTo converts a recovered panic into an error written to *err, including a stack trace.
func recoverTo(err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("vt10x: recovered panic: %v\n%s", r, debug.Stack())
	}
}
