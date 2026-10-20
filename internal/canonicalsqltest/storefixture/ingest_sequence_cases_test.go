package storefixture_test

import (
	"context"
	"encoding/binary"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestPublicIngestSequenceConflicts(t *testing.T) {
	for _, name := range []string{"first-gap", "next-gap", "fingerprint", "outcome"} {
		t.Run(name, func(t *testing.T) {
			storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
				store := f.Store.(memory.IngestSequenceStore)
				probe := newMutationProbe(t, f)

				record := ingestRecord(1)
				if name != "first-gap" {
					if err := store.SaveIngestSequence(t.Context(), record); err != nil {
						t.Fatal(err)
					}
				}

				before := probe.snapshot(t)

				switch name {
				case "first-gap", "next-gap":
					record = ingestRecord(3)
				case "fingerprint":
					record.Fingerprint, record.Outcome.Fingerprint = "changed", "changed"
				case "outcome":
					record.Outcome.Projection = memory.ProjectionStatusLagging
				}

				assertMutationCode(t, store.SaveIngestSequence(t.Context(), record), memory.CodeConflict)
				assertIngestUnchanged(t, probe, before)
			})
		})
	}
}

func TestPublicIngestSequenceBinding(t *testing.T) {
	for _, family := range []canonicalsql.Family{canonicalsql.IngestSequence, canonicalsql.IngestCursor} {
		for _, field := range []string{"producer", "scope-key", "scope-kind", "sequence", "malformed"} {
			t.Run(string(family)+"/"+field, func(t *testing.T) {
				storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
					store := f.Store.(memory.IngestSequenceStore)
					probe := newMutationProbe(t, f)

					record := ingestRecord(1)
					if err := store.SaveIngestSequence(t.Context(), record); err != nil {
						t.Fatal(err)
					}

					corruptIngestBinding(t, probe, family, record, field)

					before := probe.snapshot(t)

					var err error
					if family == canonicalsql.IngestSequence {
						_, _, err = store.LoadIngestSequence(t.Context(), record.ProducerID, record.Scope, 1)
					} else {
						_, _, err = store.LoadLatestIngestSequence(t.Context(), record.ProducerID, record.Scope)
					}

					assertMutationCode(t, err, memory.CodeStoreFailure)

					if family == canonicalsql.IngestCursor {
						record = ingestRecord(2)
					}

					assertMutationCode(t, store.SaveIngestSequence(t.Context(), record), memory.CodeStoreFailure)
					assertIngestUnchanged(t, probe, before)
				})
			})
		}
	}
}

func corruptIngestBinding(t *testing.T, probe mutationProbe, family canonicalsql.Family,
	record memory.IngestSequenceRecord, field string) {
	t.Helper()

	key := canonicalsql.Key{Family: family, Scope: record.Scope, Parts: [][]byte{[]byte(record.ProducerID)}}
	changed := record

	switch field {
	case "producer":
		changed.ProducerID, changed.Outcome.ProducerID = "foreign", "foreign"
	case "scope-key":
		changed.Scope.Key, changed.Outcome.Scope.Key = "foreign", "foreign"
	case "scope-kind":
		changed.Scope.Kind, changed.Outcome.Scope.Kind = memory.ScopeKindGroup, memory.ScopeKindGroup
	case "sequence":
		changed.Sequence, changed.Outcome.ScopeSequence = 2, 2
	case "malformed":
		changed.SchemaVersion = "invalid"
	}

	if family == canonicalsql.IngestSequence {
		key.Parts = append(key.Parts, binary.BigEndian.AppendUint64(nil, 1))
		probe.write(t, key, changed)

		return
	}

	cursor := memory.IngestSequenceCursor{SchemaVersion: changed.SchemaVersion,
		ProducerID: changed.ProducerID, Scope: changed.Scope, Latest: changed.Sequence}
	// Any nonzero cursor sequence is valid; zero is the malformed bound.
	if field == "sequence" {
		cursor.Latest = 0
	}

	probe.write(t, key, cursor)
}

func TestPublicIngestSequenceUnsigned(t *testing.T) {
	for _, latest := range []uint64{math.MaxInt64, math.MaxUint64 - 1} {
		name := "max-int64-plus-one"
		if latest == math.MaxUint64-1 {
			name = "max-uint64-overflow"
		}

		t.Run(name, func(t *testing.T) {
			storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
				store := f.Store.(memory.IngestSequenceStore)
				probe := newMutationProbe(t, f)
				record := ingestRecord(latest + 1)
				cursor := memory.IngestSequenceCursor{SchemaVersion: record.SchemaVersion,
					ProducerID: record.ProducerID, Scope: record.Scope, Latest: latest}
				probe.write(t, canonicalsql.Key{Family: canonicalsql.IngestCursor, Scope: record.Scope,
					Parts: [][]byte{[]byte(record.ProducerID)}}, cursor)

				if err := store.SaveIngestSequence(t.Context(), record); err != nil {
					t.Fatal(err)
				}

				got, found, err := store.LoadIngestSequence(t.Context(), record.ProducerID, record.Scope, record.Sequence)
				if err != nil || !found || !reflect.DeepEqual(got, record) {
					t.Fatalf("unsigned receipt=%+v found=%t err=%v", got, found, err)
				}

				before := probe.snapshot(t)
				if err := store.SaveIngestSequence(t.Context(), record); err != nil {
					t.Fatal(err)
				}

				assertMutationCode(t, store.SaveIngestSequence(t.Context(), ingestRecord(1)), memory.CodeConflict)
				assertIngestUnchanged(t, probe, before)
			})
		})
	}
}

func TestPublicIngestSequenceAtomicFailure(t *testing.T) {
	for _, table := range []string{"ingest_sequence", "ingest_cursor"} {
		t.Run(table, func(t *testing.T) {
			storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
				store := f.Store.(memory.IngestSequenceStore)
				probe := newMutationProbe(t, f)
				before := probe.snapshot(t)
				release := probe.failInserts(t, table)
				err := store.SaveIngestSequence(t.Context(), ingestRecord(1))

				release()

				if err == nil {
					t.Fatal("native receipt/cursor/stamp failure was swallowed")
				}

				assertIngestUnchanged(t, probe, before)
				assertIngestAbsent(t, store, ingestRecord(1))

				if err := store.SaveIngestSequence(t.Context(), ingestRecord(1)); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestPublicIngestSequenceExactScopeAndProducer(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		store := f.Store.(memory.IngestSequenceStore)

		for index := range 4 {
			record := ingestRecord(1)

			switch index {
			case 1:
				record.Scope.Kind = memory.ScopeKindGroup
			case 2:
				record.Scope.Key += ":other"
			case 3:
				record.ProducerID = strings.Repeat("p", 65535)
			}

			record.Outcome.Scope, record.Outcome.ProducerID = record.Scope, record.ProducerID
			assertIngestAbsent(t, store, record)

			if err := store.SaveIngestSequence(t.Context(), record); err != nil {
				t.Fatal(err)
			}

			got, found, err := store.LoadIngestSequence(t.Context(), record.ProducerID, record.Scope, 1)
			if err != nil || !found || !reflect.DeepEqual(got, record) {
				t.Fatalf("exact receipt=%+v found=%t err=%v", got, found, err)
			}
		}
	})
}

func TestPublicIngestSequenceValidationAndCancellation(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		store := f.Store.(memory.IngestSequenceStore)
		record := ingestRecord(1)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		if err := store.SaveIngestSequence(ctx, record); err != context.Canceled {
			t.Fatalf("save cancellation=%v", err)
		}

		if _, _, err := store.LoadLatestIngestSequence(ctx, " invalid ", record.Scope); err != context.Canceled {
			t.Fatalf("cursor cancellation=%v", err)
		}

		for _, producer := range []string{"", " bad", "bad\tproducer"} {
			_, _, err := store.LoadLatestIngestSequence(t.Context(), producer, record.Scope)
			assertMutationCode(t, err, memory.CodeInvalidDerived)
		}

		_, _, err := store.LoadIngestSequence(t.Context(), record.ProducerID, record.Scope, 0)
		assertMutationCode(t, err, memory.CodeInvalidDerived)

		record.Outcome.ProducerID = "foreign"
		assertMutationCode(t, store.SaveIngestSequence(t.Context(), record), memory.CodeInvalidDerived)
		assertIngestAbsent(t, store, ingestRecord(1))
	})
}

func assertIngestUnchanged(t *testing.T, probe mutationProbe, before [][][]byte) {
	t.Helper()

	if !reflect.DeepEqual(before, probe.snapshot(t)) {
		t.Fatal("rejected receipt changed rows/stamps/clock")
	}
}

func assertIngestAbsent(t *testing.T, store memory.IngestSequenceStore, record memory.IngestSequenceRecord) {
	t.Helper()

	_, found, err := store.LoadLatestIngestSequence(t.Context(), record.ProducerID, record.Scope)
	if err != nil || found {
		t.Fatalf("unexpected cursor found=%t err=%v", found, err)
	}

	_, found, err = store.LoadIngestSequence(t.Context(), record.ProducerID, record.Scope, record.Sequence)
	if err != nil || found {
		t.Fatalf("unexpected receipt found=%t err=%v", found, err)
	}
}
