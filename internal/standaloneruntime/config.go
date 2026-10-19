package standaloneruntime

import (
	"time"

	"github.com/baldaworks/boyan/internal/embedding"
	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	canonicalpostgres "github.com/baldaworks/boyan/store/postgres"
	canonicalsqlite "github.com/baldaworks/boyan/store/sqlite"

	runtimeconfig "github.com/normahq/runtime/v2/appconfig"
)

// Config controls one internal standalone runtime instance.
type Config struct {
	StateDir    string
	Canonical   CanonicalConfig
	Log         LogConfig
	HTTP        HTTPConfig
	Derivation  DerivationConfig
	Maintenance MaintenanceConfig
	Embeddings  EmbeddingsConfig
}

// CanonicalConfig carries validated selection for SQL runtime composition.
// Runtime activation is handled separately from configuration forwarding.
type CanonicalConfig struct {
	Backend  string
	SQLite   canonicalsqlite.Config
	Postgres canonicalpostgres.Config
}

// EmbeddingsConfig carries an already constructed provider, so credentials are
// resolved once at the public configuration boundary and never retained here.
type EmbeddingsConfig struct {
	Provider   embedding.Provider
	ModelSpace semanticcore.ModelSpace
}

// LogConfig controls process logging.
type LogConfig struct {
	Level string
}

// HTTPConfig controls the loopback HTTP and MCP listener.
type HTTPConfig struct {
	Listen          string
	RequestTimeout  time.Duration
	ShutdownTimeout time.Duration
	MaxBodyBytes    int64
}

// DerivationConfig controls the structured derivation runtime.
type DerivationConfig struct {
	Provider       string
	WorkingDir     string
	Timeout        time.Duration
	MaxOutputBytes int
	Runtime        runtimeconfig.RuntimeConfig
}

// MaintenanceConfig controls canonical maintenance defaults.
type MaintenanceConfig struct {
	ValueLogGCInterval time.Duration
	DiscardRatio       float64
}
