package boyantest_test

import "testing"

func requireNoError(t *testing.T, err error, message string) {
	t.Helper()

	if err != nil {
		t.Fatalf(message, err)
	}
}
