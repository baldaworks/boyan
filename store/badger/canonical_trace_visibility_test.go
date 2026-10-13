package badger //nolint:testpackage // Verifies persisted sensitivity metadata before trace payload loading.

import (
	"context"
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestTraceDeniesSensitiveCanonicalMetadata(t *testing.T) {
	for _, sensitiveSource := range []bool{false,
		true} {
		t.Run(map[bool]string{false: "revision",
			true: "source"}[sensitiveSource],
			func(t *testing.T) {
				store, err := Open(t.TempDir())
				requireTraceNoError(t, err, "open: %v")
				t.Cleanup(func() { requireTraceNoError(t, store.Close(), "close: %v") })

				scope := m.Scope{Key: "test:sensitive", Kind: m.ScopeKindPersonal}
				turn := canonicalReaderTestTurn(t, scope, time.Now().UTC())

				mutation := nativeTraceVisibilityMutation(t, turn)
				if sensitiveSource {
					mutation.Sources[0].Sensitivity = m.SensitivitySensitive
				} else {
					mutation.Revisions[0].Sensitivity = m.SensitivitySensitive
				}

				_, err = store.ApplyCanonicalMutation(context.Background(), mutation)
				requireTraceNoError(t, err, "commit: %v")
				reader, err := NewCanonicalReader(store)
				requireTraceNoError(t, err, "reader: %v")
				response,
					err := reader.Trace(context.Background(),
					m.TraceRequest{Scope: scope,
						Root: m.RevisionRef{ItemID: mutation.Items[0].ItemID,
							RevisionID: mutation.Revisions[0].RevisionID},
						MaxNodes: 10})

				code, _, ok := m.ClassifyError(err)
				if !ok || code != m.CodeForgotten || len(response.Revisions) != 0 || len(response.Sources) != 0 {
					t.Fatalf("sensitive trace disclosed content: %+v %v",
						response,
						err)
				}
			})
	}
}

func nativeTraceVisibilityMutation(t *testing.T, turn m.Turn) m.CanonicalMutation {
	t.Helper()

	evidence,
		err := m.NewEvidenceRef(turn.SourceID,
		turn.Messages[0].MessageID,
		m.MessageRoleUser,
		turn.Messages[0].Text,
		0,
		uint32(len(turn.Messages[0].Text)),
		m.AssertionModeUser)
	requireTraceNoError(t, err, "evidence: %v")

	mutation := canonicalReaderTestMutation(turn.Scope, turn.CompletedAt, evidence, turn)
	sourceBytes := canonicalReaderTestSourcePayload(t, turn)
	mutation.Sources[0].Payload = payloadRef("source", sourceBytes)
	mutation.Revisions[0].Payload = payloadRef("revision", []byte("protected memory"))

	mutation.Payloads = []m.CanonicalPayload{{Ref: mutation.Sources[0].Payload,
		Data: sourceBytes},
		{Ref: mutation.Revisions[0].Payload,
			Data: []byte("protected memory")}}
	for i, message := range turn.Messages {
		mutation.Messages[i].Payload = payloadRef(message.MessageID, []byte(message.Text))
		mutation.Payloads = append(mutation.Payloads,
			m.CanonicalPayload{Ref: mutation.Messages[i].Payload,
				Data: []byte(message.Text)})
	}

	return mutation
}
