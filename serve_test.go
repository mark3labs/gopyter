package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestServeCommandValidation(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"serve"}, "--authorized-keys is required"},
		{[]string{"serve", "--workdir", "shared"}, "--workdir can't be used with serve"},
		{[]string{"serve", "--workdir="}, "--workdir can't be used with serve"},
		{[]string{"serve", "a", "b"}, "accepts at most 1 arg"},
	} {
		cmd := rootCmd()
		cmd.SetArgs(tc.args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := cmd.ExecuteContext(ctx)
		cancel()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: %v", tc.args, err)
		}
	}
}
