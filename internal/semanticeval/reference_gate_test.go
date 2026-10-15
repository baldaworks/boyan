package semanticeval_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Configured provider errors must make the separately invoked command fail,
// even though ordinary Host recall permits lexical fallback.
func TestConfiguredReferenceFailsClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"model_id":"wrong","model_sha":"private-upstream-marker"}`))
	}))
	t.Cleanup(server.Close)

	binary, err := os.Executable()
	check(t, err)

	for _, endpoint := range []string{"malformed", "http://127.0.0.1:1/v1/embeddings", server.URL + "/v1/embeddings"} {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		command := exec.CommandContext(ctx, binary, "-test.run=^TestReferenceTEI$", "-test.v")

		for _, entry := range os.Environ() {
			if !strings.HasPrefix(entry, "BOYAN_TEI_EVAL_ENDPOINT=") {
				command.Env = append(command.Env, entry)
			}
		}

		command.Env = append(command.Env, "BOYAN_TEI_EVAL_ENDPOINT="+endpoint)
		output, runErr := command.CombinedOutput()

		cancel()

		if runErr == nil || !strings.Contains(string(output), "--- FAIL: TestReferenceTEI") {
			t.Fatal(
				"configured invalid/unavailable/mismatched provider did not fail quality command",
			)
		}

		if strings.Contains(string(output), "private-upstream-marker") {
			t.Fatal("identity probe leaked upstream prose")
		}
	}
}
