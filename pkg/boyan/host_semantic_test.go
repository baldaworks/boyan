//go:build integration

package boyan_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/boyan"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// These tests catch omission of the enabled runtime graph and default side effects.
func TestHostSemanticLifecycle(t *testing.T) {
	forEachHostBackend(t, func(t *testing.T, backend string) {
		for _, enabled := range []bool{false, true} {
			t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
				cfg := selectedHostConfig(t, backend)
				if enabled {
					cfg.Embeddings = validEmbeddingsConfig()
					cfg.Embeddings.Endpoint = "http://127.0.0.1:1"
					cfg.Embeddings.Dimensions = 2
				}

				host, err := boyan.New(context.Background(), cfg)
				requireNoError(t, err, "new: %v")
				t.Cleanup(func() { _ = host.Close() })
				requireNoError(t, host.Start(context.Background()), "start: %v")

				if !host.Ready() {
					t.Fatal("semantic availability blocked lexical readiness")
				}

				_, err = os.Stat(filepath.Join(cfg.StateDir, "search", "search.sqlite"))
				if err != nil {
					t.Fatalf("shared search database: %v", err)
				}

				rec := httptest.NewRecorder()
				host.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

				if rec.Code != 200 {
					t.Fatalf("readiness=%d", rec.Code)
				}

				if enabled && rec.Header().Get("X-Boyan-Semantic-State") == "" {
					t.Fatal("missing semantic header")
				}

				if !enabled && rec.Header().Get("X-Boyan-Semantic-State") != "" {
					t.Fatal("disabled graph changed readiness headers")
				}

				requireNoError(t, host.Close(), "close: %v")
			})
		}
	})
}

// Blocking passage inference proves startup does not wait on the provider;
// subsequent failures prove diagnostics do not turn lexical readiness off.
//
//nolint:cyclop,funlen,gocognit // One real lifecycle demonstrates the ordered repair/fallback/recovery transitions.
func TestHostSemanticRepairFallbackRecoveryAndRestart(t *testing.T) {
	forEachHostBackend(t, func(t *testing.T, backend string) {
		cfg := selectedHostConfig(t, backend)
		turn, err := memory.NewTurn(memory.Scope{Kind: memory.ScopeKindPersonal, Key: "semantic:lifecycle"},
			memory.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"},
			"turn", time.Now().UTC().Add(-time.Hour), "Я живу в Бишкеке", "OK")
		requireNoError(t, err, "turn: %v")
		initial := persistIntegrationTurn(t, cfg, turn)
		entered := make(chan struct{}, 1)
		release := make(chan struct{})

		var mode atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer construction-secret" {
				t.Error("credential was not resolved at construction")
			}

			var request struct {
				Input []string `json:"input"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)

				return
			}

			if len(request.Input) > 0 && len(request.Input[0]) >= 9 && request.Input[0][:9] == "passage: " {
				select {
				case entered <- struct{}{}:
				default:
				}

				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
			}

			switch mode.Load() {
			case 1:
				http.Error(w, "credential-secret upstream source text", http.StatusServiceUnavailable)

				return
			case 2:
				_, _ = w.Write([]byte(`{"data":"private malformed prose"}`))

				return
			}

			rows := make([]map[string]any, len(request.Input))
			for i := range rows {
				rows[i] = map[string]any{"index": i, "embedding": []float32{1, 0}}
			}

			_ = json.NewEncoder(w).Encode(map[string]any{"model": "intfloat/multilingual-e5-base", "data": rows})
		}))
		t.Cleanup(server.Close)

		cfg.Embeddings = validEmbeddingsConfig()
		cfg.Embeddings.Endpoint = server.URL
		cfg.Embeddings.Dimensions = 2
		cfg.Embeddings.APIKeyEnv = "BOYAN_SEMANTIC_LIFECYCLE_KEY"
		t.Setenv(cfg.Embeddings.APIKeyEnv, "construction-secret")
		host, err := boyan.New(context.Background(), cfg)
		requireNoError(t, err, "new: %v")
		t.Setenv(cfg.Embeddings.APIKeyEnv, "changed-after-construction")
		t.Cleanup(func() { _ = host.Close() })
		requireNoError(t, host.Start(context.Background()), "start: %v")

		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("startup repair not scheduled")
		}

		if !host.Ready() {
			t.Fatal("blocked inference disabled lexical readiness")
		}

		assertSemanticHeader(t, host, "repairing")

		request := memory.RecallRequest{Scope: turn.Scope, Query: "живу", Limit: 10}
		response, err := host.Search(context.Background(), request)
		requireNoError(t, err, "cold recall: %v")

		if len(response.Results) != 1 || response.Retrieval == nil || response.Retrieval.Effective != "degraded" {
			t.Fatalf("cold response: %+v", response)
		}

		request.MinScopeChangeSeq = initial.ChangeSeq
		_, err = host.Search(context.Background(), request)

		code, _, _ := memory.ClassifyError(err)
		if code != memory.CodeConflict {
			t.Fatalf("cold minimum: %v", err)
		}

		request.MinScopeChangeSeq = 0

		close(release)
		waitHostSemantic(t, host, turn.Scope, "hybrid-ready")

		for _, failure := range []int32{1, 2, 0} {
			mode.Store(failure)

			response, err = host.Search(context.Background(), request)
			requireNoError(t, err, "recall: %v")

			want := "hybrid"
			state := "hybrid-ready"

			if failure != 0 {
				want = "degraded"
				state = "degraded"
			}

			if len(response.Results) != 1 || response.Retrieval == nil || response.Retrieval.Effective != want {
				t.Fatalf("mode %d response: %+v", failure, response)
			}

			assertSemanticHeader(t, host, state)
			status, statusErr := host.SemanticStatus(context.Background(), turn.Scope)
			requireNoError(t, statusErr, "status: %v")

			encoded, _ := json.Marshal(status)
			if bytes.Contains(encoded, []byte("private")) || bytes.Contains(encoded, []byte("credential")) ||
				bytes.Contains(encoded, []byte("живу")) {
				t.Fatalf("status leaked: %s", encoded)
			}
		}

		requireNoError(t, host.Close(), "close: %v")
		t.Setenv(cfg.Embeddings.APIKeyEnv, "construction-secret")
		host, err = boyan.New(context.Background(), cfg)
		requireNoError(t, err, "restart: %v")
		requireNoError(t, host.Start(context.Background()), "restart start: %v")
		waitHostSemantic(t, host, turn.Scope, "hybrid-ready")
		_, err = host.ForgetScope(context.Background(), memory.ForgetScopeCommand{
			SchemaVersion: memory.DerivedSchemaVersionV1, Scope: turn.Scope,
			RequestID: "forget-semantic", ForgottenAt: time.Now().UTC(),
		})
		requireNoError(t, err, "forget: %v")
		response, err = host.Search(context.Background(), request)
		requireNoError(t, err, "forgotten recall: %v")

		if len(response.Results) != 0 {
			t.Fatalf("forgotten text released: %+v", response)
		}

		canceled, cancel := context.WithCancel(context.Background())
		cancel()

		_, err = host.SemanticStatus(canceled, turn.Scope)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled status: %v", err)
		}
	})
}

func assertSemanticHeader(t *testing.T, host *boyan.Host, want string) {
	t.Helper()

	rec := httptest.NewRecorder()
	host.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != 200 || rec.Header().Get("X-Boyan-Semantic-State") != want {
		t.Fatalf("readiness: %d %v want %s", rec.Code, rec.Header(), want)
	}
}

func waitHostSemantic(t *testing.T, host *boyan.Host, scope memory.Scope, want string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, err := host.SemanticStatus(context.Background(), scope)
		requireNoError(t, err, "status: %v")

		if status.State == want {
			return
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatalf("semantic status never reached %s", want)
}
