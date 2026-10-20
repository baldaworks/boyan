//go:build integration

package semanticeval_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode"

	"github.com/baldaworks/boyan/pkg/boyan"
	m "github.com/baldaworks/boyan/pkg/memory"
)

// Removing the semantic channel must break the paraphrase controls; bypassing
// canonical filters or forgetting must break the lifecycle checks.
func TestDeterministicHybridCorpus(t *testing.T) {
	forEachEvaluationBackend(t, func(t *testing.T, backend string) {
		var outage atomic.Bool

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if outage.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)

				return
			}

			var req struct {
				Model    string   `json:"model"`
				Input    []string `json:"input"`
				Encoding string   `json:"encoding_format"`
			}
			if json.NewDecoder(io.LimitReader(r.Body, 65537)).Decode(&req) != nil ||
				req.Model != referenceModel ||
				req.Encoding != "float" {
				w.WriteHeader(http.StatusBadRequest)

				return
			}

			rows := make([]map[string]any, len(req.Input))
			for i, input := range req.Input {
				rows[i] = map[string]any{"index": i, "embedding": wordVector(input)}
			}

			_ = json.NewEncoder(w).Encode(map[string]any{"model": referenceModel, "data": rows})
		}))
		t.Cleanup(server.Close)
		runCorpus(t, selectEvaluationBackend(t, config(t, server.URL, 16), backend), &outage)
	})
}

// wordVector is a bag of bilingual concept words. It has no access to fixture
// IDs, expected results, complete queries or production ranking functions.
// Its role is deterministic pipeline coverage, not model-quality evidence.
func wordVector(text string) []float32 {
	groups := []string{
		"mira",
		"tea beverage drink breakfast morning зеленый чай",
		"unsweetened sugar sweet without",
		"coffee night avoids",
		"антон",
		"велосипеде велосипед bicycle commute добирается работа работу офиса пробках заторов избегая дорожных",
		"ремонтирует выходным машине car hobby",
		"production postgresql database",
		"backups backup nightly резервные копии сохраняют",
		"logs rotate replica disabled",
		"лена лены лене",
		"allergy аллергия арахис peanut нельзя исключить",
		"recipe рецепт гостей sauce",
		"nora",
		"bishkek lives botanical garden",
		"osh moved residence",
	}

	v := make([]float32, len(groups))

	tokens := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	for _, token := range tokens {
		for i, group := range groups {
			if strings.Contains(" "+group+" ", " "+token+" ") {
				v[i]++
			}
		}
	}

	v[0] += 0.01 // Nonzero unknown text without granting it relevance.

	return v
}

func TestReferenceTEI(t *testing.T) {
	endpoint := os.Getenv("BOYAN_TEI_EVAL_ENDPOINT")
	if endpoint == "" {
		t.Skip("BOYAN_TEI_EVAL_ENDPOINT unset: real-provider quality NOT evaluated")
	}

	verifyReference(t, endpoint)

	var outage atomic.Bool

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if outage.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		request, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)

			return
		}

		request.Header.Set("Content-Type", "application/json")

		response, err := referenceClient().Do(request)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)

			return
		}

		defer func() { _ = response.Body.Close() }()

		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, io.LimitReader(response.Body, 1<<20+1))
	}))
	t.Cleanup(proxy.Close)
	runCorpus(t, config(t, proxy.URL, 768), &outage)
}

//nolint:cyclop // The reference identity probe fails closed on every malformed field.
func verifyReference(t *testing.T, endpoint string) {
	t.Helper()

	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Scheme != "http" && u.Scheme != "https") {
		t.Fatal("invalid reference endpoint")
	}

	u.Path = "/info"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	check(t, err)

	client := referenceClient()

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal("reference identity probe unavailable")
	}

	defer func() { _ = resp.Body.Close() }()

	var info struct {
		Model    string `json:"model_id"`
		Revision string `json:"model_sha"`
	}
	if resp.StatusCode != http.StatusOK ||
		json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&info) != nil ||
		info.Model != referenceModel ||
		info.Revision != referenceRevision {
		t.Fatal("reference identity probe mismatch or invalid response")
	}
}

func referenceClient() *http.Client {
	return &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

type ranking struct {
	ID                string
	Lexical, Semantic float64
}
type observation struct {
	Ranking []ranking
	Report  m.RetrievalReport
}

func evaluate(t *testing.T, host *boyan.Host, ids map[string]string) map[string]observation {
	t.Helper()

	observations := make(map[string]observation)

	for _, q := range queries {
		request := m.RecallRequest{Scope: scope, Query: q.text, Limit: q.topK}
		began := time.Now()
		response, err := host.Search(context.Background(), request)
		elapsed := time.Since(began).Milliseconds()

		check(t, err)
		check(t, response.Validate(request))

		if response.Retrieval == nil || response.Retrieval.Effective != "hybrid" {
			t.Fatalf("fixture=%s expected hybrid", q.id)
		}

		checkCertified(t, host, request, response.ScopeChangeSeq)

		found := false

		obs := observation{Report: *response.Retrieval}
		for _, result := range response.Results {
			found = found || result.RevisionID == ids[q.want]
			obs.Ranking = append(
				obs.Ranking,
				ranking{result.RevisionID, result.Explain.Lexical, result.Explain.Semantic},
			)
		}

		if q.id == "semantic-only" && response.Retrieval.LexicalCandidates != 0 {
			t.Fatal("fixture=semantic-only unexpectedly has lexical candidates")
		}

		t.Logf(
			"fixture=%s top_k=%d hit=%t elapsed_ms=%d",
			q.id,
			q.topK,
			found,
			elapsed,
		)

		if !found {
			t.Errorf(
				"fixture=%s expected_revision=%s absent from top_%d",
				q.id,
				ids[q.want],
				q.topK,
			)
		}

		observations[q.id] = obs
	}

	return observations
}

func checkCertified(t *testing.T, host *boyan.Host, request m.RecallRequest, minimum uint64) {
	t.Helper()

	request.MinScopeChangeSeq = minimum
	response, err := host.Search(context.Background(), request)
	check(t, err)

	if response.ScopeChangeSeq < minimum || response.Retrieval == nil || response.Retrieval.Effective != "hybrid" {
		t.Fatal("healthy minimum not certified")
	}
}

//nolint:funlen // Keep the ordered durable lifecycle visible in one test helper.
func runCorpus(t *testing.T, cfg boyan.Config, outage *atomic.Bool) {
	t.Helper()
	ids := seed(t, cfg, documents)
	host := start(t, cfg)

	first := evaluate(t, host, ids)
	if !reflect.DeepEqual(first, evaluate(t, host, ids)) {
		t.Fatal("repeated channel rankings or reports differ")
	}

	checkFilters(t, host)

	if outage != nil {
		checkOutage(t, host, outage)
	}

	check(t, host.Close())

	host = start(t, cfg)
	if !reflect.DeepEqual(first, evaluate(t, host, ids)) {
		t.Fatal("reopened channel rankings or reports differ")
	}

	check(t, host.Close())

	updated := document{
		"residence-updated",
		"Nora moved to Osh and now lives near the central market.",
		"nora-residence",
	}
	newIDs := seed(t, cfg, []document{updated})
	host = start(t, cfg)
	response, err := host.Search(
		context.Background(),
		m.RecallRequest{Scope: scope, Query: "Nora lives Osh", Limit: 3},
	)
	check(t, err)
	assertAbsent(t, response, ids["residence"])

	found := false
	for _, result := range response.Results {
		found = found || result.RevisionID == newIDs[updated.id]
	}

	if !found {
		t.Fatal("fixture=update current revision absent from top_3")
	}

	_, err = host.ForgetScope(
		context.Background(),
		m.ForgetScopeCommand{
			SchemaVersion: m.DerivedSchemaVersionV1,
			Scope:         scope,
			RequestID:     "public-eval-forget",
			ForgottenAt:   time.Now().UTC(),
		},
	)
	check(t, err)
	response, err = host.Search(
		context.Background(),
		m.RecallRequest{Scope: scope, Query: "Nora", Limit: 3},
	)
	check(t, err)

	if len(response.Results) != 0 {
		t.Fatal("fixture=delete forgotten scope returned results")
	}
}

func assertAbsent(t *testing.T, response m.RecallResponse, id string) {
	t.Helper()

	for _, result := range response.Results {
		if result.RevisionID == id {
			t.Fatal("fixture=update obsolete revision returned")
		}
	}
}

func checkFilters(t *testing.T, host *boyan.Host) {
	t.Helper()

	kind := m.MemoryKindEvent
	before := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	requests := []m.RecallRequest{
		{Scope: m.Scope{Kind: m.ScopeKindPersonal, Key: "eval:foreign"}, Query: "tea", Limit: 3},
		{Scope: scope, Query: "tea", Limit: 3, ConversationID: "foreign-conversation"},
		{Scope: scope, Query: "tea", Limit: 3, SourceID: "foreign-source"},
		{Scope: scope, Query: "tea", Limit: 3, Kind: &kind},
		{Scope: scope, Query: "tea", Limit: 3, AsOf: &before},
	}
	for i, req := range requests {
		response, err := host.Search(context.Background(), req)
		check(t, err)

		if len(response.Results) != 0 {
			t.Fatalf("fixture=filter-%d exclusion failed", i)
		}
	}
}

func checkOutage(t *testing.T, host *boyan.Host, outage *atomic.Bool) {
	t.Helper()

	outage.Store(true)
	defer outage.Store(false)

	req := m.RecallRequest{Scope: scope, Query: "unsweetened green tea", Limit: 1}
	response, err := host.Search(context.Background(), req)
	check(t, err)

	if len(response.Results) != 1 || response.Retrieval == nil ||
		response.Retrieval.Effective != "degraded" ||
		!host.Ready() {
		t.Fatal("fixture=outage lexical fallback failed")
	}

	req.MinScopeChangeSeq = response.ScopeChangeSeq
	_, err = host.Search(context.Background(), req)

	code, _, _ := m.ClassifyError(err)
	if code != m.CodeConflict {
		t.Fatal("fixture=outage uncertified minimum did not conflict")
	}
}
