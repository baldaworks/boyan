package boyan_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/boyan"
	"github.com/normahq/runtime/v2/agentconfig"
	runtimeconfig "github.com/normahq/runtime/v2/appconfig"
)

func ExampleNewHost() {
	root, err := os.MkdirTemp("", "boyan-host-example-")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(root) }()

	config := boyan.DefaultConfig()
	// Canonical authority defaults to SQLite, separately from the search file.
	// Stop/Close and construct a new host with the same configuration to reopen.
	// For PostgreSQL use config.Canonical.Backend = "postgres" and supply the
	// private DSN via BOYAN_POSTGRES_DSN; search remains local SQLite.
	config.StateDir = filepath.Join(root, "state")
	config.HTTP.Listen = "127.0.0.1:0"
	config.Derivation.Provider = "memory"
	config.Derivation.WorkingDir = root
	config.Derivation.Runtime = runtimeconfig.RuntimeConfig{
		Providers: map[string]agentconfig.Config{
			"memory": {
				Type:     agentconfig.AgentTypeCodexACP,
				CodexACP: &agentconfig.ACPConfig{Model: "memory-fast"},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	host, err := boyan.NewHost(ctx, config)
	if err != nil {
		fmt.Println(false)

		return
	}
	defer func() { _ = host.Close() }()

	if err := host.Start(ctx); err != nil {
		fmt.Println(false)

		return
	}

	fmt.Println(host.Ready())

	// Output:
	// true
}
