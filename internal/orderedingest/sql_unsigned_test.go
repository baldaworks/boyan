//go:build integration

package orderedingest_test

import (
	"database/sql"
	"encoding/binary"
	"math"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	"github.com/baldaworks/boyan/internal/orderedingest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestSQLOrderedUnsignedReceiptReplay(t *testing.T) {
	for _, sequence := range []uint64{math.MaxInt64 + 1, math.MaxUint64} {
		name := "max-int64-plus-one"
		if sequence == math.MaxUint64 {
			name = "max-uint64"
		}

		t.Run(name, func(t *testing.T) {
			storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
				store := f.Store.(sqlIngestStore)
				counts := &sqlProcessingCounts{}
				runtime := newSQLIngestRuntime(t, store, counts, false)
				t.Cleanup(func() { _ = runtime.Close(t.Context()) })

				request := sqlIngestTurn(t)
				if _, err := runtime.IngestSequencedTurn(t.Context(), request); err != nil {
					t.Fatal(err)
				}

				record, found, err := store.LoadIngestSequence(t.Context(), request.ProducerID, request.Turn.Scope, 1)
				if err != nil || !found {
					t.Fatalf("initial receipt found=%t err=%v", found, err)
				}

				record.Sequence, record.Outcome.ScopeSequence = sequence, sequence
				seedSQLUnsignedReceipt(t, f, record)

				request.ScopeSequence = sequence

				duplicate, err := runtime.IngestSequencedTurn(t.Context(), request)
				if err != nil {
					t.Fatalf("full unsigned duplicate lost: %v", err)
				}

				assertSQLRecoveredOutcome(t, duplicate, sequence, 1, memory.IngestDispositionDuplicate)
				assertSQLExhaustedReplayBounds(t, runtime, request)

				if counts.turnAttempts != 1 || counts.extraction != 1 {
					t.Fatalf("unsigned replay repeated processor/model work: %+v", counts)
				}

				assertSQLCanonicalCount(t, store, request.Turn.Scope, 1)
			})
		})
	}
}

func assertSQLExhaustedReplayBounds(t *testing.T, runtime *orderedingest.SequencedIngestRuntime,
	request memory.SequencedTurn) {
	t.Helper()

	request.ScopeSequence--
	_, err := runtime.IngestSequencedTurn(t.Context(), request)
	requireSQLCode(t, err, memory.CodeConflict, memory.ErrorClassPermanent)

	request.ScopeSequence = 0
	_, err = runtime.IngestSequencedTurn(t.Context(), request)
	requireSQLCode(t, err, memory.CodeInvalidDerived, memory.ErrorClassPermanent)

	request.ScopeSequence = 1
	duplicate, err := runtime.IngestSequencedTurn(t.Context(), request)
	requireSQLSuccess(t, err)
	assertSQLRecoveredOutcome(t, duplicate, 1, 1, memory.IngestDispositionDuplicate)
}

func seedSQLUnsignedReceipt(t *testing.T, fixture *storefixture.Fixture, record memory.IngestSequenceRecord) {
	t.Helper()

	var connection *canonicalsql.Connection
	if fixture.Database.Postgres != nil {
		connection = canonicalsql.NewPostgres(fixture.Database.Postgres)
	} else {
		db, err := sql.Open("sqlite", fixture.Database.DSN)
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = db.Close() })

		conn, err := db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = conn.Close() })

		connection = canonicalsql.NewSQLite(conn)
	}

	err := connection.Update(t.Context(), func(tx *canonicalsql.Tx) error {
		data, err := canonicalsql.Encode(canonicalsql.IngestSequence, record)
		if err != nil {
			return err
		}

		if err := tx.Put(t.Context(), canonicalsql.Key{Family: canonicalsql.IngestSequence, Scope: record.Scope,
			Parts: [][]byte{[]byte(record.ProducerID), binary.BigEndian.AppendUint64(nil, record.Sequence)}}, data); err != nil {
			return err
		}

		cursor := memory.IngestSequenceCursor{SchemaVersion: record.SchemaVersion,
			ProducerID: record.ProducerID, Scope: record.Scope, Latest: record.Sequence}

		data, err = canonicalsql.Encode(canonicalsql.IngestCursor, cursor)
		if err != nil {
			return err
		}

		return tx.Put(t.Context(), canonicalsql.Key{Family: canonicalsql.IngestCursor, Scope: record.Scope,
			Parts: [][]byte{[]byte(record.ProducerID)}}, data)
	})
	if err != nil {
		t.Fatal(err)
	}
}
