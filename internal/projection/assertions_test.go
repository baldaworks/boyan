package projection //nolint:testpackage // Shares scenario fixtures with private package tests.

import "testing"

func requireNoError(t *testing.T, err error, message string) {
	t.Helper()

	if err != nil {
		t.Fatalf(message, err)
	}
}
