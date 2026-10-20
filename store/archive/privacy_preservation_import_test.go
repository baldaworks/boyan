package archive_test

import (
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// A checksum-correct stream must not let invalid source metadata acquire authority.
func TestSQLPrivacyPreservationMalformedImports(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, source *storefixture.Fixture) {
		m, _ := privacyPreservationSeed(t, source)

		for _, logical := range []bool{false, true} {
			route := "native"
			if logical {
				route = "logical"
			}

			t.Run(route, func(t *testing.T) {
				for _, fault := range []string{"version", "state", "scope", "source", "payload", "member",
					"complete-empty", "stage", "scope-flag", "ready"} {
					t.Run(fault, func(t *testing.T) {
						privacyPreservationMalformedImport(t, source, m, logical, fault)
					})
				}
			})
		}
	})
}

func privacyPreservationCorrupt(value map[string]any, m memory.CanonicalMutation, fault string) {
	switch fault {
	case "version", "scope-flag", "ready":
		value["version"] = 99
	case "state":
		value["state"] = "unsupported"
	case "scope":
		value["scope"] = memory.Scope{Key: m.Scope.Key, Kind: memory.ScopeKindGroup}
	case "source":
		value["source_id"] = "wrong-source"
	case "payload":
		value["payload"] = m.Revisions[0].Payload
	case "member":
		value["message_ids"] = []string{"missing-member"}
		value["message_membership_complete"] = true
	case "complete-empty":
		value["message_ids"] = []string{}
		value["message_membership_complete"] = true
	case "stage":
		value["sticky_unknown"] = true
	}
}

func TestSQLPrivacyPreservationPartialAggregateRetry(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, source *storefixture.Fixture) {
		m, _ := privacyPreservationSeed(t, source)

		for _, logical := range []bool{false, true} {
			route := "native"
			if logical {
				route = "logical"
			}

			t.Run(route, func(t *testing.T) {
				frames, good := privacyPreservationArtifact(t, source, logical)
				partial := []map[string]any{frames[0], privacyPreservationFrame(t, frames,
					canonicalsql.SourcePrivacy), frames[len(frames)-1]}

				d := restoreDestination(t, source.Backend)
				if err := restoreJournalArtifact(t, d.Store.(restoreStore), privacyPreservationEncode(t, partial,
					logical), logical); err == nil {
					t.Fatal("orphan aggregate served")
				}

				privacyPreservationUnavailable(t, d, m)

				if err := restoreJournalArtifact(t, d.Store.(restoreStore), good, logical); err != nil {
					t.Fatal("complete source retry: ", err)
				}

				privacyPreservationTrace(t, d, m, "")
			})
		}
	})
}

func TestSQLPrivacyPreservationOriginalBeforeMerge(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, source *storefixture.Fixture) {
		m, _ := privacyPreservationSeed(t, source)
		for _, fault := range []string{"stage", "different-payload"} {
			t.Run(fault, func(t *testing.T) {
				frames, good := privacyPreservationArtifact(t, source, false)

				d := restoreDestination(t, source.Backend)
				if err := restoreJournalArtifact(t, d.Store.(restoreStore), good, false); err != nil {
					t.Fatal(err)
				}

				privacyPreservationRestrict(t, d, m, "sensitive")
				frame := privacyPreservationFrame(t, frames, canonicalsql.SourcePrivacy)

				value := privacyPreservationFrameValue(t, frame)
				if fault == "stage" {
					privacyPreservationCorrupt(value, m, fault)
				} else {
					// Reuse an existing valid payload, so missing bytes cannot mask
					// a changed immutable source/privacy binding.
					value["payload"] = m.Revisions[0].Payload
					sourceFrame := privacyPreservationFrame(t, frames, canonicalsql.Source)
					sourceValue := privacyPreservationFrameValue(t, sourceFrame)
					sourceValue["payload"] = m.Revisions[0].Payload
					privacyPreservationSetFrame(t, sourceFrame, canonicalsql.Source, sourceValue)
				}

				privacyPreservationSetFrame(t, frame, canonicalsql.SourcePrivacy, value)

				changed := privacyPreservationEncode(t, frames, false)
				if fault == "different-payload" {
					control := restoreDestination(t, source.Backend)
					if err := restoreJournalArtifact(t, control.Store.(restoreStore), changed, false); err != nil {
						t.Fatal("independent changed-payload source control rejected: ", err)
					}

					if err := control.Store.(restoreStore).VerifyCanonicalIntegrity(t.Context()); err != nil {
						t.Fatal("changed-payload control integrity: ", err)
					}
				}

				if err := restoreJournalArtifact(t, d.Store.(restoreStore), changed, false); err == nil {
					t.Fatal("restrictive destination healed original invalidity/binding change")
				}

				privacyPreservationNoSpool(t)
				privacyPreservationUnavailable(t, d, m)
			})
		}
	})
}

func privacyPreservationRestrict(t *testing.T, f *storefixture.Fixture, m memory.CanonicalMutation, kind string) {
	t.Helper()

	p := newArchiveProbe(t, f)
	if kind == "scope" {
		privacyPreservationWrite(t, p, privacyPreservationKey(canonicalsql.SourcePrivacyScope, m, "coverage"),
			map[string]any{"version": 1, "scope": m.Scope, "incomplete": true})

		return
	}

	key := privacyPreservationKey(canonicalsql.SourcePrivacy, m, m.Sources[0].SourceID)
	value := privacyPreservationRead(t, p, key)
	value["state"], value["sticky_unknown"] = kind, kind == "unknown"
	privacyPreservationWrite(t, p, key, value)
}

func privacyPreservationMalformedImport(
	t *testing.T, source *storefixture.Fixture, m memory.CanonicalMutation, logical bool, fault string,
) {
	t.Helper()

	frames, _ := privacyPreservationArtifact(t, source, logical)

	family := canonicalsql.SourcePrivacy
	if fault == "scope-flag" {
		family = canonicalsql.SourcePrivacyScope
	}

	if fault == "ready" {
		family = canonicalsql.SourcePrivacyReady
	}

	frame := privacyPreservationFrame(t, frames, family)
	value := privacyPreservationFrameValue(t, frame)
	privacyPreservationCorrupt(value, m, fault)
	privacyPreservationSetFrame(t, frame, family, value)

	d := restoreDestination(t, source.Backend)
	if err := restoreJournalArtifact(t, d.Store.(restoreStore), privacyPreservationEncode(t, frames,
		logical), logical); err == nil {
		t.Fatal("malformed original privacy metadata accepted")
	}

	privacyPreservationNoSpool(t)
	privacyPreservationUnavailable(t, d, m)

	if err := d.Store.(restoreStore).PrepareSourcePrivacy(t.Context()); err == nil {
		t.Fatal("malformed loading input prepared")
	}
}
