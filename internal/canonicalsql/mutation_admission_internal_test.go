//go:build integration

package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"sync"
	"testing"
	"time"
)

// The public stores delegate directly to this same admitted mutation method.
// Pause a real privacy page under its retained admission, not a fake store port.
func TestMutationExcludedByPrivacyPageAdmission(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
		if err := owner.PrepareSourcePrivacy(t.Context()); err != nil {
			t.Fatal(err)
		}

		h := newPrivacyHistory(t)

		mutation := historyMutation(h)
		if _, err := owner.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
			t.Fatal(err)
		}

		ctx, release, err := owner.admit(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		var once sync.Once
		defer once.Do(release)

		entered := make(chan struct{})
		resume := make(chan struct{})

		prepared := make(chan error, 1)
		go func() {
			prepared <- owner.preparePrivacyFamily(ctx, Revision, func(context.Context, *Tx, Key, memory.Scope, string) error {
				close(entered)
				<-resume

				return nil
			})
		}()

		<-entered

		writer, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()

		_, writeErr := owner.ApplyCanonicalMutation(writer, mutation)

		close(resume)

		if err := <-prepared; err != nil {
			t.Fatal(err)
		}

		if writeErr != context.DeadlineExceeded {
			t.Fatalf("writer crossed preparation admission: %v", writeErr)
		}

		once.Do(release)

		if _, err := owner.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
			t.Fatal(err)
		}
	})
}
