package main

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"testing"
)

func TestExitCode(t *testing.T) {
	failed := errors.New("1 cell failed")
	killed := fmt.Errorf("program was killed: %w", context.Canceled)
	for _, c := range []struct {
		err, cause error
		want       int
	}{
		{nil, nil, 0},
		{failed, nil, 1},
		{failed, context.Canceled, 1},
		{killed, signalError{syscall.SIGINT}, 130},
		{killed, signalError{syscall.SIGTERM}, 143},
		{nil, signalError{syscall.SIGTERM}, 0}, // a clean shutdown (serve)
	} {
		if got := exitCode(c.err, c.cause); got != c.want {
			t.Errorf("exitCode(%v, %v) = %d, want %d", c.err, c.cause, got, c.want)
		}
	}
}
