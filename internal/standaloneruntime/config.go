package standaloneruntime

import (
	"time"

	runtimeconfig "github.com/normahq/runtime/v2/appconfig"
)

// Config controls one internal standalone runtime instance.
type Config struct {
	StateDir    string
	Log         LogConfig
	HTTP        HTTPConfig
	Derivation  DerivationConfig
	Maintenance MaintenanceConfig
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
