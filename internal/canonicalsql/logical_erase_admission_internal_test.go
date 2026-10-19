package canonicalsql

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// A live destination erase must refuse a fresh logical import before reading
// input or changing any canonical, loading, lineage, or source capability state.
func TestLogicalPendingEraseAdmission(t *testing.T) {
	forEachOwner(t, func(t *testing.T, source *Owner, _ func() *Owner) {
		scope := testScope()
		sourceMarker(t, source, scope)
		request := memory.ScopeEraseRequest{Scope: scope, OperationID: "logical-admission",
			Fingerprint: "logical-admission", ForgottenAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

		original, err := source.BeginScopeErase(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}

		logical := logicalAdmissionBytes(t, source)
		full, end := sourceBackup(t, source, 0)
		destination, _ := restoreSourceDestination(t, source)

		if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(full), 1); err != nil {
			t.Fatal("native pending full: ", err)
		}

		assertLogicalPendingRefusal(t, destination, original, logical)

		if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(full), 1); err != nil {
			t.Fatal("native full while destination erase pending: ", err)
		}

		if _, err := source.RunScopeErase(t.Context(), request); err != nil {
			t.Fatal(err)
		}

		delta, _ := sourceBackup(t, source, end)
		if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(delta), 1); err != nil {
			t.Fatal("native recovery delta: ", err)
		}

		completed := logicalAdmissionBytes(t, source)
		if err := destination.ImportCanonicalLogical(t.Context(), bytes.NewReader(completed)); err != nil {
			t.Fatal("logical import after erase recovery: ", err)
		}
	})
}

func assertLogicalPendingRefusal(t *testing.T, owner *Owner, original memory.ScopeEraseJournal, logical []byte) {
	t.Helper()

	before := snapshotLogicalAdmission(t, owner)
	if before.control != nil || before.chain == nil || len(before.sources) != 1 {
		t.Fatal("native pending full did not publish source lineage")
	}

	input := &logicalAdmissionReader{Reader: bytes.NewReader(logical)}
	assertCode(t, owner.ImportCanonicalLogical(t.Context(), input), memory.CodeConflict)

	if input.reads != 0 {
		t.Fatal("refused logical import consumed input")
	}

	if !reflect.DeepEqual(before, snapshotLogicalAdmission(t, owner)) {
		t.Fatal("refused logical import changed canonical or restore state")
	}

	got, found, err := owner.LoadScopeErase(t.Context(), original.Scope, original.OperationID)
	if err != nil || !found || !reflect.DeepEqual(original, got) {
		t.Fatalf("refused logical import changed journal: %+v %v", got, err)
	}
}

type logicalAdmissionState struct {
	canonical      []byte
	control, chain *restoreControl
	sources        []restoreSourceRow
}

func snapshotLogicalAdmission(t *testing.T, owner *Owner) logicalAdmissionState {
	t.Helper()

	control, chain := sourceControlSnapshot(t, owner)

	return logicalAdmissionState{canonical: logicalAdmissionBytes(t, owner), control: control,
		chain: chain, sources: sourceRowsSnapshot(t, owner)}
}

func logicalAdmissionBytes(t *testing.T, owner *Owner) []byte {
	t.Helper()

	var output bytes.Buffer
	if err := owner.ExportCanonicalLogical(t.Context(), &output); err != nil {
		t.Fatal(err)
	}

	return output.Bytes()
}

type logicalAdmissionReader struct {
	*bytes.Reader

	reads int
}

func (r *logicalAdmissionReader) Read(p []byte) (int, error) {
	r.reads++

	return r.Reader.Read(p)
}
