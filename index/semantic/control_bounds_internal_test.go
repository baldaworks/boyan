package semantic

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

const maximumControlValueBytes = 64 << 10

func TestMaximumEscapedScopeReadyGenerationRoundTripsAfterReopen(t *testing.T) {
	t.Parallel()

	directory := filepath.Join(t.TempDir(), "embeddings")
	store, err := Open(directory)
	internalRequireNoError(t, err)

	generation, coverage, chunks := internalFixture("escaped-scope-ready", "bleve-ready", 1)
	generation.Scope = maximumEscapedScope(4096)
	chunks[0].Scope = generation.Scope

	_, err = store.Stage(context.Background(), generation, coverage, chunks)
	internalRequireNoError(t, err)
	internalRequireNoError(t, store.Publish(context.Background(), Publication{
		View:    generation.View(),
		BuildID: generation.BuildID,
	}))

	snapshot, found, err := store.Snapshot(context.Background(), generation.View())
	internalRequireNoError(t, err)

	if !found || snapshot.State().Scope != generation.Scope {
		t.Fatalf("Snapshot(maximum escaped scope) = (%#v, %t), want exact scope", snapshot, found)
	}

	internalRequireNoError(t, store.Close())

	reopened, err := Open(directory)
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, reopened.Close()) })

	snapshot, found, err = reopened.Snapshot(context.Background(), generation.View())
	internalRequireNoError(t, err)

	if !found || snapshot.State().Scope != generation.Scope {
		t.Fatalf("Snapshot(maximum escaped scope after reopen) = (%#v, %t), want exact scope", snapshot, found)
	}
}

func TestMaximumEscapedScopeDegradedStateIsReadable(t *testing.T) {
	t.Parallel()

	directory := filepath.Join(t.TempDir(), "embeddings")
	store, err := Open(directory)
	internalRequireNoError(t, err)

	generation, _, _ := internalFixture("escaped-scope-degraded", "bleve-degraded", 1)
	generation.Scope = maximumEscapedScope(4096)

	written, err := store.PublishDegraded(context.Background(), Degraded{
		Generation: generation,
		SafeReason: semanticcore.ReasonProjectionCapacity,
	}, "")
	internalRequireNoError(t, err)

	active, found, err := store.ActiveState(context.Background(), generation.Scope)
	internalRequireNoError(t, err)

	if !found || active != written {
		t.Fatalf("ActiveState(maximum escaped scope) = (%#v, %t), want (%#v, true)", active, found, written)
	}

	internalRequireNoError(t, store.Close())

	reopened, err := Open(directory)
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, reopened.Close()) })

	active, found, err = reopened.ActiveState(context.Background(), generation.Scope)
	internalRequireNoError(t, err)

	if !found || active != written {
		t.Fatalf("ActiveState(maximum escaped scope after reopen) = (%#v, %t), want (%#v, true)",
			active, found, written)
	}
}

func TestScopeOverCanonicalStoreLimitIsRejected(t *testing.T) {
	t.Parallel()

	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	generation, coverage, chunks := internalFixture("scope-over", "bleve-over", 1)
	generation.Scope = maximumEscapedScope(4097)
	chunks[0].Scope = generation.Scope

	if _, err := store.Stage(context.Background(), generation, coverage, chunks); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Stage(4097-byte scope) error = %v, want ErrInvalid", err)
	}

	if _, err := store.PublishDegraded(context.Background(), Degraded{
		Generation: generation,
		SafeReason: semanticcore.ReasonProjectionCapacity,
	}, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("PublishDegraded(4097-byte scope) error = %v, want ErrInvalid", err)
	}
}

func TestActiveStateAcceptsMaximumCanonicalScope(t *testing.T) {
	t.Parallel()

	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	state, found, err := store.ActiveState(context.Background(), maximumEscapedScope(4096))
	internalRequireNoError(t, err)

	if found || state != (State{}) {
		t.Fatalf("ActiveState(4096-byte scope) = (%#v, %t), want (zero, false)", state, found)
	}
}

func TestActiveStateRejectsScopeOverCanonicalStoreLimitBeforeStoreAccess(t *testing.T) {
	t.Parallel()

	var store *Store

	if _, _, err := store.ActiveState(
		context.Background(), maximumEscapedScope(4097),
	); !errors.Is(err, ErrInvalid) {
		t.Fatalf("ActiveState(4097-byte scope) error = %v, want ErrInvalid", err)
	}
}

func TestValidateScopeRejectsOversizedUppercaseChannelBeforeCanonicalValidationAllocation(t *testing.T) {
	scope := memory.Scope{
		Key:  strings.Repeat("A", maxScopeKeyBytes) + ":x",
		Kind: memory.ScopeKindPersonal,
	}
	if err := validateScope(scope); !errors.Is(err, ErrInvalid) {
		t.Fatalf("validateScope(oversized uppercase channel) error = %v, want ErrInvalid", err)
	}

	if allocations := testing.AllocsPerRun(100, func() {
		_ = validateScope(scope)
	}); allocations != 0 {
		t.Fatalf("validateScope(oversized uppercase channel) allocations = %v, want 0", allocations)
	}
}

func TestWorstCaseBoundedControlRecordsFitDocumentedLimit(t *testing.T) {
	t.Parallel()

	// JSON may expand each byte of a bounded caller identity to six bytes as
	// \u00xx. This record exercises every such field at its accepted maximum;
	// hashes, enums, timestamps, and numbers have fixed smaller encodings.
	escapedIdentity := strings.Repeat("\x01", maxIdentityBytes)
	state := State{
		SchemaVersion:       schemaVersion,
		Scope:               maximumEscapedScope(4096),
		BuildID:             escapedIdentity,
		LexicalGenerationID: escapedIdentity,
		LexicalWatermark:    ^uint64(0),
		ModelSpace:          internalHash("f"),
		Dimensions:          4096,
		Status:              StatusDegraded,
		SafeReason:          string(semanticcore.ReasonProjectionCapacity),
		PublicationEpoch:    ^uint64(0),
		ChunkCount:          8192,
		RecordCount:         8192,
		EncodedBytes:        uint64(semanticcore.ProjectionByteLimit),
		CoverageHash:        internalHash("e"),
		Omitted:             OmittedCounts{Ineligible: ^uint32(0), Expired: ^uint32(0), Sensitive: ^uint32(0)},
		ReadyAt:             internalFixtureTime(),
	}

	stateValue, err := encodeJSON(storedState{SchemaVersion: schemaVersion, Complete: true, State: state})
	internalRequireNoError(t, err)
	pointerValue, err := encodeJSON(activePointer{
		SchemaVersion: schemaVersion,
		ScopeKey:      state.Scope.Key,
		ScopeKind:     string(state.Scope.Kind),
		BuildID:       state.BuildID,
		Epoch:         state.PublicationEpoch,
	})
	internalRequireNoError(t, err)

	for name, value := range map[string][]byte{"state": stateValue, "active pointer": pointerValue} {
		t.Logf("worst-case bounded %s encoded length: %d bytes", name, len(value))

		if len(value) <= 16<<10 {
			t.Fatalf("worst-case %s encoded length = %d, want proof it exceeds obsolete 16 KiB cap", name, len(value))
		}

		if len(value) > maximumControlValueBytes {
			t.Fatalf("worst-case %s encoded length = %d, exceeds documented 64 KiB cap", name, len(value))
		}
	}
}

func TestControlRecordEncodersRejectOversizeBeforePersistence(t *testing.T) {
	t.Parallel()

	oversized := strings.Repeat("x", maxControlValueBytes)

	if _, err := encodeStoredState(storedState{State: State{BuildID: oversized}}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("encodeStoredState(oversize) error = %v, want ErrCapacity", err)
	}

	if _, err := encodeActivePointer(activePointer{BuildID: oversized}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("encodeActivePointer(oversize) error = %v, want ErrCapacity", err)
	}
}

func maximumEscapedScope(size int) memory.Scope {
	return memory.Scope{
		Key:  "a:" + strings.Repeat("\x01", size-2),
		Kind: memory.ScopeKindPersonal,
	}
}

func internalFixtureTime() time.Time {
	return time.Date(9999, time.December, 31, 23, 59, 59, 999999999, time.UTC)
}
