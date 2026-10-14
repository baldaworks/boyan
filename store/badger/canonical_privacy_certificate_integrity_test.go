package badger //nolint:testpackage // Exercises raw certificate authority below public restore APIs.

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func TestPrivacyCertificateIntegrityBindsActualRawCapture(t *testing.T) {
	for _, kind := range []string{"native-header", "message-payload", "certificate-subset"} {
		t.Run(kind, func(t *testing.T) {
			store, mutation := privacyFixture(t)
			certifyPrivacyHistory(t, &mutation)
			_, err := store.ApplyCanonicalMutation(context.Background(), mutation)
			requireTraceNoError(t, err, "commit: %v")
			requireTraceNoError(t, store.VerifyCanonicalIntegrity(context.Background()), "valid certificate: %v")

			err = store.db.Update(func(txn *badger.Txn) error {
				if kind == "native-header" {
					return replaceCertifiedPrivacyHeader(txn, mutation)
				}

				if kind == "certificate-subset" {
					certificate := mutation.TurnIdentity.Certificate
					certificate.MessageIDs = certificate.MessageIDs[:1]

					key, keyErr := canonicalKey(mutation.Scope, badgerRecordTurnCertificate, certificate.SourceID)
					if keyErr != nil {
						return keyErr
					}

					return putCanonicalRecord(txn, key, badgerRecordTurnCertificate, certificate)
				}

				key, keyErr := payloadRecordKey(mutation.Messages[0].Payload)
				if keyErr != nil {
					return keyErr
				}

				return putCanonicalRecord(txn, key, badgerRecordPayload, []byte("different raw message"))
			})
			requireTraceNoError(t, err, "corrupt: %v")

			if err := store.VerifyCanonicalIntegrity(context.Background()); err == nil {
				t.Fatal("certificate accepted inconsistent actual raw capture")
			}

			var exported bytes.Buffer
			if err := store.ExportCanonicalLogical(context.Background(), &exported); err == nil {
				t.Fatal("inconsistent actual raw capture exported")
			}
		})
	}
}

func replaceCertifiedPrivacyHeader(txn *badger.Txn, mutation memory.CanonicalMutation) error {
	source := mutation.Sources[0]

	record, _, err := loadSourcePrivacy(txn, source)
	if err != nil {
		return err
	}

	var header canonicalTurnSourcePayload
	if err := json.Unmarshal(mutation.Payloads[0].Data, &header); err != nil {
		return err
	}

	header.ExportID = "different-native-export"

	data, err := json.Marshal(header)
	if err != nil {
		return err
	}

	source.Payload = payloadRef("different-native-header", data)

	record.Payload = source.Payload
	if err := writeSourcePrivacy(txn, record); err != nil {
		return err
	}

	key, err := canonicalKey(source.Scope, badgerRecordSource, source.SourceID)
	if err != nil {
		return err
	}

	if err := putCanonicalRecord(txn, key, badgerRecordSource, source); err != nil {
		return err
	}

	key, err = payloadRecordKey(source.Payload)
	if err != nil {
		return err
	}

	return putCanonicalRecord(txn, key, badgerRecordPayload, data)
}
