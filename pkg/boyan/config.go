// Package boyan contains the public non-Fx host composition API for Boyan.
package boyan

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"time"

	runtimeconfig "github.com/normahq/runtime/v2/appconfig"
)

const (
	defaultStateDir                       = "./.state/boyan"
	defaultHTTPListenAddress              = "127.0.0.1:8686"
	defaultHTTPMaxBodyBytes         int64 = 1 << 20
	defaultHTTPRequestTimeout             = 15 * time.Second
	defaultShutdownTimeout                = 10 * time.Second
	defaultDerivationTimeout              = 30 * time.Second
	defaultDerivationMaxOutputBytes       = 262144
	defaultCanonicalGCInterval            = 6 * time.Hour
	defaultCanonicalDiscard               = 0.5
)

// Config controls one local Boyan host.
type Config struct {
	StateDir    string            `mapstructure:"state_dir"`
	Log         LogConfig         `mapstructure:"log"`
	HTTP        HTTPConfig        `mapstructure:"http"`
	Derivation  DerivationConfig  `mapstructure:"derivation"`
	Maintenance MaintenanceConfig `mapstructure:"maintenance"`
}

// LogConfig controls process logging.
type LogConfig struct {
	Level string `mapstructure:"level"`
}

// HTTPConfig controls the loopback HTTP and MCP listener.
type HTTPConfig struct {
	Listen          string        `mapstructure:"listen"`
	RequestTimeout  time.Duration `mapstructure:"request_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	MaxBodyBytes    int64         `mapstructure:"max_body_bytes"`
}

// DerivationConfig controls the structured derivation runtime.
type DerivationConfig struct {
	Provider       string                      `mapstructure:"provider"`
	WorkingDir     string                      `mapstructure:"working_dir"`
	Timeout        time.Duration               `mapstructure:"timeout"`
	MaxOutputBytes int                         `mapstructure:"max_output_bytes"`
	Runtime        runtimeconfig.RuntimeConfig `mapstructure:"runtime"`
}

// MaintenanceConfig controls canonical maintenance defaults.
type MaintenanceConfig struct {
	ValueLogGCInterval time.Duration `mapstructure:"value_log_gc_interval"`
	DiscardRatio       float64       `mapstructure:"discard_ratio"`
}

// DefaultConfig returns conservative local defaults for one Boyan host.
func DefaultConfig() Config {
	return Config{
		StateDir: defaultStateDir,
		Log: LogConfig{
			Level: "info",
		},
		HTTP: HTTPConfig{
			Listen:          defaultHTTPListenAddress,
			RequestTimeout:  defaultHTTPRequestTimeout,
			ShutdownTimeout: defaultShutdownTimeout,
			MaxBodyBytes:    defaultHTTPMaxBodyBytes,
		},
		Derivation: DerivationConfig{
			WorkingDir:     ".",
			Timeout:        defaultDerivationTimeout,
			MaxOutputBytes: defaultDerivationMaxOutputBytes,
		},
		Maintenance: MaintenanceConfig{
			ValueLogGCInterval: defaultCanonicalGCInterval,
			DiscardRatio:       defaultCanonicalDiscard,
		},
	}
}

// Validate checks configuration without opening files, sockets, or databases.
func (config Config) Validate() error {
	_, err := config.normalized()

	return err
}

func (config Config) normalized() (Config, error) {
	defaults := DefaultConfig()

	stateDir := strings.TrimSpace(config.StateDir)
	if stateDir == "" {
		stateDir = defaults.StateDir
	}

	absStateDir, err := filepath.Abs(stateDir)
	if err != nil {
		return Config{}, fmt.Errorf("resolve Boyan state directory: %w", err)
	}

	config.StateDir = filepath.Clean(absStateDir)

	config.Log.Level = strings.TrimSpace(config.Log.Level)
	if config.Log.Level == "" {
		config.Log.Level = defaults.Log.Level
	}

	if strings.TrimSpace(config.HTTP.Listen) == "" {
		config.HTTP.Listen = defaults.HTTP.Listen
	}

	if err := validateLoopbackListenAddress(config.HTTP.Listen); err != nil {
		return Config{}, err
	}

	if config.HTTP.RequestTimeout <= 0 {
		config.HTTP.RequestTimeout = defaults.HTTP.RequestTimeout
	}

	if config.HTTP.ShutdownTimeout <= 0 {
		config.HTTP.ShutdownTimeout = defaults.HTTP.ShutdownTimeout
	}

	if config.HTTP.MaxBodyBytes <= 0 {
		config.HTTP.MaxBodyBytes = defaults.HTTP.MaxBodyBytes
	}

	return config.normalizeDerivation(defaults)
}

func (config Config) normalizeDerivation(defaults Config) (Config, error) {
	config.Derivation.Provider = strings.TrimSpace(config.Derivation.Provider)
	if strings.TrimSpace(config.Derivation.WorkingDir) == "" {
		config.Derivation.WorkingDir = defaults.Derivation.WorkingDir
	}

	workingDir, err := filepath.Abs(config.Derivation.WorkingDir)
	if err != nil {
		return Config{}, fmt.Errorf("resolve Boyan derivation working directory: %w", err)
	}

	config.Derivation.WorkingDir = filepath.Clean(workingDir)
	if config.Derivation.Timeout <= 0 {
		config.Derivation.Timeout = defaults.Derivation.Timeout
	}

	if config.Derivation.MaxOutputBytes <= 0 {
		config.Derivation.MaxOutputBytes = defaults.Derivation.MaxOutputBytes
	}

	if config.Maintenance.ValueLogGCInterval <= 0 {
		config.Maintenance.ValueLogGCInterval = defaults.Maintenance.ValueLogGCInterval
	}

	if config.Maintenance.DiscardRatio <= 0 || config.Maintenance.DiscardRatio >= 1 {
		config.Maintenance.DiscardRatio = defaults.Maintenance.DiscardRatio
	}

	return config, nil
}

func validateLoopbackListenAddress(addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return fmt.Errorf("Boyan listener address is required")
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("Boyan listener address %q must include a port: %w", addr, err)
	}

	host = strings.Trim(host, "[]")
	switch strings.ToLower(host) {
	case "localhost":
		return nil
	case "":
		return fmt.Errorf("Boyan listener address %q must bind loopback only", addr)
	}

	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("Boyan listener address %q must bind loopback only", addr)
	}

	return nil
}
