package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunContextHelpListsStandaloneCommands(t *testing.T) {
	t.Parallel()

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)
	requireNoError(
		t,
		runContext(
			context.Background(),
			[]string{"--help"},
			&stdout,
			&stderr,
		),
		"runContext(--help) error = %v",
	)

	output := stdout.String()
	for _, needle := range []string{"serve", "standalone local service"} {
		if !strings.Contains(output, needle) {
			t.Fatalf("help output missing %q:\n%s", needle, output)
		}
	}
}

func TestServeCommandHelpExposesLoopbackFlags(t *testing.T) {
	t.Parallel()

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)
	requireNoError(
		t,
		runContext(
			context.Background(),
			[]string{
				"serve",
				"--help",
			},
			&stdout,
			&stderr,
		),
		"runContext(serve --help) error = %v",
	)

	output := stdout.String()
	for _, needle := range []string{"/livez", "/mcp", "--listen", "--state-dir"} {
		if !strings.Contains(output, needle) {
			t.Fatalf("serve help output missing %q:\n%s", needle, output)
		}
	}
}
