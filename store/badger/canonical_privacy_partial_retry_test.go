package badger //nolint:testpackage // Reproduces valid partial aggregate ordering across actual import retry.

import (
	"bytes"
	"context"
	"testing"

	m "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4/pb"
)

func TestPrivacyLoadingPartialAggregateAllowsCompleteRetry(t *testing.T) {
	for _, logical := range []bool{false, true} {
		t.Run(restoreCaseName(logical, "partial-aggregate"), func(t *testing.T) {
			source, mutation := privacyFixture(t)
			if _, err := source.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
				t.Fatal(err)
			}

			destination := privacyRestoreDestination(t)

			var partial []byte

			if logical {
				key, _ := canonicalKey(mutation.Scope, badgerRecordSourcePrivacy, mutation.Sources[0].SourceID)
				kv := nativeAggregateKV(t, source, mutation, sourcePrivacyStandard, 100)

				components, _, err := decodeCanonicalLogicalKey(key)
				if err != nil {
					t.Fatal(err)
				}

				partial = proofLogicalStream(t, CanonicalLogicalExportRecord{Kind: canonicalLogicalRecordKind,
					RecordType: badgerRecordSourcePrivacy, Components: components, Envelope: kv.Value})
			} else {
				var stream bytes.Buffer

				list := &pb.KVList{Kv: []*pb.KV{nativeAggregateKV(t, source, mutation, sourcePrivacyStandard, 100)}}
				if err := writeNativePrivacyFrame(&stream, list); err != nil {
					t.Fatal(err)
				}

				partial = stream.Bytes()
			}

			if err := applyPrivacyRestore(destination, logical, partial); err == nil {
				t.Fatal("orphan partial aggregate published")
			}

			assertRestoreUnavailable(t, destination, mutation)

			if err := applyPrivacyRestore(destination, logical, privacyRestoreBytes(t, source, logical)); err != nil {
				t.Fatalf("complete stream could not finish loading target: %v", err)
			}

			assertPrivacyRestoredOutcome(t, destination, mutation, "native-standard")
		})
	}
}

func TestNativePrivacyRejectsRetainedEmbeddedSubset(t *testing.T) {
	source, mutation := privacyFixture(t)
	embedPrivacyHistory(t, &mutation)

	if _, err := source.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	stream := bytes.NewBuffer(nativePrivacyBackup(t, source, 0))
	full := nativeAggregateKV(t, source, mutation, sourcePrivacyStandard, 200)

	envelope, err := m.UnmarshalRecordEnvelope(full.Value)
	if err != nil {
		t.Fatal(err)
	}

	subset := decodePrivacyAggregate(envelope)
	subset.MessageIDs = subset.MessageIDs[:1]

	old := nativePrivacyKVFixture(t, full.Key, badgerRecordSourcePrivacy, subset, 100)
	for _, kv := range []*pb.KV{old, full} {
		if err := writeNativePrivacyFrame(stream, &pb.KVList{Kv: []*pb.KV{kv}}); err != nil {
			t.Fatal(err)
		}
	}

	destination := privacyRestoreDestination(t)
	if err := applyPrivacyRestore(destination, false, stream.Bytes()); err == nil {
		t.Fatal("invalid retained completed embedded capture healed by newer metadata")
	}

	assertRestoreUnavailable(t, destination, mutation)
}
