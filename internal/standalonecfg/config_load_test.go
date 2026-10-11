package standalonecfg_test

import standalonecfg "github.com/baldaworks/boyan/internal/standalonecfg"

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProviderEnvironmentPrecedence(t *testing.T) {
	for _, test := range []struct{ name, yaml, environment, want string }{
		{name: "env_only", environment: "memory", want: "memory"},
		{
			name:        "absent_yaml_key",
			yaml:        "log:\n  level: info\n",
			environment: "memory",
			want:        "memory",
		},
		{
			name:        "environment_overrides_yaml",
			yaml:        "derivation:\n  provider: yaml\n",
			environment: "memory",
			want:        "memory",
		},
		{
			name: "yaml_without_environment",
			yaml: "derivation:\n  provider: yaml\n",
			want: "yaml",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("BOYAN_DERIVATION_PROVIDER", test.environment)

			path := ""
			if test.yaml != "" {
				path = filepath.Join(t.TempDir(), "config.yaml")
				requireNoError(t, os.WriteFile(path, []byte(test.yaml), 0600), "%v")
			}

			config, err := standalonecfg.LoadConfig(path)
			requireNoError(t, err, "%v")

			if config.Derivation.Provider != test.want {
				t.Fatalf("provider %q, want %q", config.Derivation.Provider, test.want)
			}
		})
	}
}
