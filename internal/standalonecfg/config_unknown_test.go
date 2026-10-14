package standalonecfg_test

import (
	"os"
	"path/filepath"
	"testing"

	standalonecfg "github.com/baldaworks/boyan/internal/standalonecfg"
)

func TestLoadConfigRejectsUnknownKeys(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		yaml string
	}{
		{name: "top level", yaml: "state_di: /tmp/boyan\n"},
		{name: "nested", yaml: "http:\n  requset_timeout: 1s\n"},
		{
			name: "provider",
			yaml: "derivation:\n  runtime:\n    providers:\n      memory:\n        type: openai\n        unknow: true\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "boyan.yaml")
			requireNoError(t, os.WriteFile(path, []byte(tc.yaml), 0o600), "WriteFile() error = %v")

			if _, err := standalonecfg.LoadConfig(path); err == nil {
				t.Fatal("LoadConfig() error = nil, want unknown-key rejection")
			}
		})
	}
}
