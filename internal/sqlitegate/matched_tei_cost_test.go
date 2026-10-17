//nolint:cyclop,funlen,lll // Keep the fixed local-provider identity, resource qualifiers and observations adjacent.
package sqlitegate_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/embedding"
	"github.com/baldaworks/boyan/internal/memoryread"
	core "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/internal/semanticprojection"
	m "github.com/baldaworks/boyan/pkg/memory"
)

// Actual pinned-provider observations stay separate from deterministic matched
// retrieval. Only the fixed public authored corpus is sent to this local model.
func TestMatchedActualTEICost(t *testing.T) {
	if os.Getenv("BOYAN_TASK19_TEI") != "1" {
		t.Skip("explicit existing-local-TEI measurement")
	}

	client := &http.Client{Timeout: 10 * time.Second}

	response, err := client.Get("http://127.0.0.1:8091/info")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	var info struct {
		ModelID  string `json:"model_id"`
		ModelSHA string `json:"model_sha"`
		Version  string `json:"version"`
	}
	if err := json.NewDecoder(response.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}

	if response.StatusCode != http.StatusOK || info.ModelID != "intfloat/multilingual-e5-base" || info.ModelSHA != "d128750597153bb5987e10b1c3493a34e5a4502a" || info.Version != "1.9.0" {
		t.Fatalf("reference identity mismatch: %+v", info)
	}

	provider, err := embedding.NewOpenAIProvider(embedding.OpenAIOptions{Endpoint: "http://127.0.0.1:8091/v1/embeddings", Model: info.ModelID, Dimensions: 768})
	if err != nil {
		t.Fatal(err)
	}

	c, backend := matchedFixture(t, "tei9")
	b := backend.(*matchedSQLite)
	space := core.ModelSpace{Model: info.ModelID, Revision: info.ModelSHA, Dimensions: 768, QueryPrefix: "query: ", PassagePrefix: "passage: "}

	manager, err := semanticprojection.New(semanticprojection.Dependencies{Canonical: c.canonical, Records: c.reader, Manifests: c.canonical, Physical: b.store, Provider: provider, Store: b.store, Lifecycle: b.store, LexicalProjectionID: "sqlite", ModelSpace: space})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())

	if err := matchedSample("ActualTEI/PassageBuild", func() error { return manager.Repair(t.Context(), c.scope) }); err != nil {
		t.Fatal(err)
	}

	hybrid, err := semanticprojection.NewHybrid(semanticprojection.HybridDependencies{Canonical: c.canonical, Manifests: c.canonical, Reader: b.store, Lexical: b.store, Provider: provider, ModelSpace: space})
	if err != nil {
		t.Fatal(err)
	}

	service, err := memoryread.NewRecallService(c.reader, hybrid)
	if err != nil {
		t.Fatal(err)
	}

	query := []string{"query: engineer run test"}
	if _, err := provider.Embed(t.Context(), query); err != nil {
		t.Fatal(err)
	}

	for range 10 {
		if err := matchedSample("ActualTEI/ProviderOneQuery", func() error {
			vectors, err := provider.Embed(t.Context(), query)
			if err == nil && (len(vectors) != 1 || len(vectors[0]) != 768) {
				return fmt.Errorf("provider dimensions changed")
			}

			return err
		}); err != nil {
			t.Fatal(err)
		}
	}

	request := m.RecallRequest{Scope: c.scope, Query: "engineer run test", Limit: 10, AsOf: &matchedAt}

	for range 10 {
		if err := matchedSample("ActualTEI/FullConsumer", func() error {
			result, err := service.Search(t.Context(), request)
			if err == nil && (result.Retrieval == nil || result.Retrieval.Effective != "hybrid") {
				return fmt.Errorf("real provider did not serve ready hybrid")
			}

			return err
		}); err != nil {
			t.Fatal(err)
		}
	}

	fingerprint, _ := core.SpaceFingerprint(space)
	t.Logf("TEI existing_shared_loaded_endpoint=true model=%s revision=%s version=%s dimensions=768 model_space=%s corpus=authored9 cpu=2 memory=4GiB threads=2 server_concurrency=16 reference_server_concurrency=4 host_loopback=8091 cache_placement=existing_uncontrolled sequential=true single_operation_samples=true", info.ModelID, info.ModelSHA, info.Version, fingerprint)
}
