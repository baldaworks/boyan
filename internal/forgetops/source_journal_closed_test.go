//go:build integration

package forgetops_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	m "github.com/baldaworks/boyan/pkg/memory"
)

func sourceJournalClosedLoad(t *testing.T, backend canonicalsqltest.Backend) {
	t.Helper()
	store, command, _, _ := sourceJournalFixture(t, true, backend)

	before := sourceJournalBytes(t, store)

	resolutions, erases := store.resolutions, store.erases
	sourceJournalNoError(t, store.Close())

	outcome, err := sourceJournalAdapter(t, store).ForgetSource(context.Background(), command)
	sourceJournalSQLClosedError(t, err)

	if !reflect.DeepEqual(before, sourceJournalBytes(t, store)) {
		t.Fatal("closed owner rejection mutated canonical state")
	}

	if !reflect.DeepEqual(outcome, m.ForgetOutcome{}) || store.resolutions != resolutions || store.erases != erases {
		t.Fatal("closed journal load returned an outcome or entered resolver/erase")
	}
}

func sourceJournalSQLClosedError(t *testing.T, err error) {
	t.Helper()

	var classified *m.Error

	if !errors.As(err, &classified) || classified.Code != m.CodeUnavailable ||
		classified.Class != m.ErrorClassRetryable || classified.Cause != nil ||
		classified.Message != "canonical SQL owner unavailable" {
		t.Fatalf("want sanitized retryable SQL unavailable, got %v", err)
	}
}
