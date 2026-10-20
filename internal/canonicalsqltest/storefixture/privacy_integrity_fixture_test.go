//go:build integration

package storefixture_test

import (
	"encoding/json"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func corruptPublicPrivacyIntegrity(t *testing.T, p mutationProbe, m memory.CanonicalMutation, kind string) {
	t.Helper()

	key := canonicalsql.Key{Family: canonicalsql.SourcePrivacy, Scope: m.Scope,
		Parts: [][]byte{[]byte(m.Sources[0].SourceID)}}

	var privacy map[string]any
	p.read(t, m.Scope, canonicalsql.SourcePrivacy, m.Sources[0].SourceID, &privacy)

	switch kind {
	case "native-header", "message-payload", "certificate-subset":
		corruptCertifiedPublicCapture(t, p, m, privacy, kind)

		return
	case "scope-flag":
		key.Family, key.Parts = canonicalsql.SourcePrivacyScope, [][]byte{[]byte("coverage")}
		privacy = map[string]any{"version": 99, "scope": m.Scope}
	case "ready":
		key = canonicalsql.Key{Family: canonicalsql.SourcePrivacyReady}
		privacy = map[string]any{"version": 99, "coverage_version": 1}
	default:
		alterPublicPrivacyAggregate(privacy, m, kind)
	}

	p.write(t, key, privacy)
}

func alterPublicPrivacyAggregate(privacy map[string]any, m memory.CanonicalMutation, kind string) {
	switch kind {
	case "version":
		privacy["version"] = 99
	case "state":
		privacy["state"] = "unsupported"
	case "scope":
		scope := m.Scope
		scope.Kind = memory.ScopeKindGroup
		privacy["scope"] = scope
	case "source":
		privacy["source_id"] = "wrong-source"
	case "payload":
		privacy["payload"] = m.Revisions[0].Payload
	case "member":
		privacy["message_ids"] = []string{"missing-member"}
		privacy["message_membership_complete"] = true
	case "complete-empty":
		privacy["message_ids"] = nil
		privacy["message_membership_complete"] = true
	case "stage":
		privacy["sticky_unknown"] = true
	case "embedded-subset":
		privacy["message_ids"] = []string{m.Messages[0].MessageID}
		privacy["message_membership_complete"] = true
	}
}

func corruptCertifiedPublicCapture(t *testing.T, p mutationProbe, m memory.CanonicalMutation,
	privacy map[string]any, kind string,
) {
	t.Helper()

	if kind == "message-payload" {
		p.write(t, canonicalsql.Key{Family: canonicalsql.Payload,
			Parts: [][]byte{[]byte(m.Messages[0].Payload.ID)}}, []byte("different raw message"))

		return
	}

	if kind == "certificate-subset" {
		certificate := m.TurnIdentity.Certificate
		certificate.MessageIDs = certificate.MessageIDs[:1]
		p.write(t, canonicalsql.Key{Family: canonicalsql.TurnCertificate, Scope: m.Scope,
			Parts: [][]byte{[]byte(certificate.SourceID)}}, certificate)

		return
	}

	var header map[string]any
	requireProjectionNoError(t, json.Unmarshal(m.Payloads[0].Data, &header))
	header["export_id"] = "different-native-export"
	data, err := json.Marshal(header)
	requireProjectionNoError(t, err)

	source := m.Sources[0]
	source.Payload = privacyRef("different-native-header", data)
	privacy["payload"] = source.Payload
	p.write(t, canonicalsql.Key{Family: canonicalsql.SourcePrivacy, Scope: m.Scope,
		Parts: [][]byte{[]byte(source.SourceID)}}, privacy)
	p.write(t, canonicalsql.Key{Family: canonicalsql.Source, Scope: m.Scope,
		Parts: [][]byte{[]byte(source.SourceID)}}, source)
	p.write(t, canonicalsql.Key{Family: canonicalsql.Payload, Parts: [][]byte{[]byte(source.Payload.ID)}}, data)
}
