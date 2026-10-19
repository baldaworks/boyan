package memoryread_test

import (
	"context"
	"testing"
	"time"

	core "github.com/baldaworks/boyan/internal/canonicalcore"
	read "github.com/baldaworks/boyan/internal/memoryread"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
)

type authorityExtractor struct{}

func (authorityExtractor) ExtractCanonicalSemantics(ctx context.Context,
	request m.CanonicalExtractionRequest) ([]m.SemanticCandidate, error) {
	candidates, err := (interleavingExtractor{}).ExtractCanonicalSemantics(ctx, request)
	if err == nil {
		candidates[0].Subject = "stable subject"
	}

	return candidates, err
}

type changingSemanticProjection struct {
	recallTestProjection

	before func()
}

func (p changingSemanticProjection) SearchRecall(ctx context.Context,
	request m.RecallRequest) ([]m.RecallProjectionHit, error) {
	if p.before != nil {
		p.before()
	}

	return p.recallTestProjection.SearchRecall(ctx, request)
}

//nolint:gocognit,cyclop,funlen // Branches change authority between search and hydration over a real store.
func TestSemanticOnlyCanonicalAuthorityDuringChanges(t *testing.T) {
	runRecallAuthorities(t, func(t *testing.T, newCanonical recallOwnerFactory) {
		for _, mode := range []string{"control", "missing", "foreign", "superseded", "forget source", "forget scope"} {
			t.Run(mode, func(t *testing.T) {
				ctx := context.Background()

				store, reader := func() (recallOwner, m.RecallCanonicalReader) {
					if newCanonical != nil {
						return newCanonical(t)
					}

					store, err := b.OpenCanonicalStore(t.TempDir())
					if err != nil {
						t.Fatal(err)
					}

					reader, err := b.NewCanonicalReader(store)
					if err != nil {
						t.Fatal(err)
					}

					return store, reader
				}()

				t.Cleanup(func() { _ = store.Close() })

				processor, err := core.NewCanonicalTurnProcessor(
					store, authorityExtractor{}, m.PolicyRegistry{Version: "policy-v1"})
				if err != nil {
					t.Fatal(err)
				}

				scope := m.Scope{Key: "test:semantic-authority", Kind: m.ScopeKindPersonal}
				at := time.Now().UTC().Add(-time.Hour)
				commit := func(scope m.Scope, id, text string) m.Turn {
					conversation := m.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"}

					turn, turnErr := m.NewTurn(scope, conversation, id, at, text, "OK")
					if turnErr != nil {
						t.Fatal(turnErr)
					}

					if _, processErr := processor.ProcessTurn(ctx, turn, m.LegacyDerivationRef()); processErr != nil {
						t.Fatal(processErr)
					}

					return turn
				}
				turn := commit(scope, "first", "original canonical statement")

				active, err := store.ScanActiveMemory(ctx, m.ActiveMemoryScanRequest{Scope: scope, Limit: 10})
				if err != nil || len(active) != 1 {
					t.Fatalf("active fixture: %+v %v", active, err)
				}

				hit := m.RecallProjectionHit{Scope: scope, RevisionID: active[0].RevisionID, Semantic: 1.0 / 61}

				var before func()

				switch mode {
				case "missing":
					hit.RevisionID = "missing-revision"
				case "foreign":
					foreign := m.Scope{Key: "test:foreign", Kind: m.ScopeKindPersonal}
					commit(foreign, "foreign", "foreign canonical statement")

					other, scanErr := store.ScanActiveMemory(ctx, m.ActiveMemoryScanRequest{Scope: foreign, Limit: 10})
					if scanErr != nil || len(other) != 1 {
						t.Fatalf("foreign fixture: %+v %v", other, scanErr)
					}

					hit.RevisionID = other[0].RevisionID
				case "superseded":
					before = func() { commit(scope, "second", "replacement canonical statement") }
				case "forget source", "forget scope":
					before = func() {
						if mode == "forget source" {
							if denyErr := store.DenySource(ctx, scope, turn.SourceID, time.Now().UTC()); denyErr != nil {
								t.Fatal(denyErr)
							}

							return
						}

						state, stateErr := store.LoadScopeState(ctx, scope)
						if stateErr != nil {
							t.Fatal(stateErr)
						}

						request := m.ScopeEraseRequest{Scope: scope, OperationID: "forget", ExpectedScopeVersion: state.Version,
							Fingerprint: "fingerprint", ForgottenAt: time.Now().UTC()}
						if _, forgetErr := store.RunScopeErase(ctx, request); forgetErr != nil {
							t.Fatal(forgetErr)
						}
					}
				}

				projection := changingSemanticProjection{
					recallTestProjection: recallTestProjection{hits: []m.RecallProjectionHit{hit}}, before: before}

				service, err := read.NewRecallService(reader, projection)
				if err != nil {
					t.Fatal(err)
				}

				request := m.RecallRequest{Scope: scope, Query: "unmatched paraphrase", Limit: 10}

				response, err := service.Search(ctx, request)
				if mode == "control" {
					if err != nil || len(response.Results) != 1 || response.Results[0].Text != "original canonical statement" ||
						response.Results[0].Trust != m.ReferenceTrustUntrusted || response.Validate(request) != nil {
						t.Fatalf("canonical control: %+v %v", response, err)
					}
				} else if len(response.Results) != 0 {
					t.Fatalf("stale semantic identity disclosed: %+v %v", response, err)
				}
			})
		}
	})
}
