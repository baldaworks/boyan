package boyan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/baldaworks/boyan/internal/standaloneruntime"
	"github.com/baldaworks/boyan/store/postgres"
	"github.com/baldaworks/boyan/store/sqlite"
)

// ErrInvalidCanonicalConfig reports invalid canonical selection without values
// that might contain credentials or connection parser diagnostics.
var ErrInvalidCanonicalConfig = errors.New("Boyan canonical configuration is invalid")

const (
	defaultPostgresDSNEnv         = "BOYAN_POSTGRES_DSN"
	defaultPostgresConnectTimeout = 5 * time.Second
	maxPostgresConnectTimeout     = 30 * time.Second
)

// CanonicalConfig selects authoritative SQL storage, separately from search.
type CanonicalConfig struct {
	Backend  string                  `mapstructure:"backend"`
	SQLite   CanonicalSQLiteConfig   `mapstructure:"sqlite"`
	Postgres CanonicalPostgresConfig `mapstructure:"postgres"`
}

// CanonicalSQLiteConfig selects an optional ordinary file path. Empty Path uses
// state_dir/canonical/canonical.sqlite. Zero BusyTimeout selects one second.
type CanonicalSQLiteConfig struct {
	Path        string        `mapstructure:"path"`
	BusyTimeout time.Duration `mapstructure:"busy_timeout"`
}

// CanonicalPostgresConfig names the environment secret, never the resolved DSN.
// Remote TLS must verify identity. Local sslmode=disable requires explicit consent.
type CanonicalPostgresConfig struct {
	DSNEnv             string        `mapstructure:"dsn_env"`
	ConnectTimeout     time.Duration `mapstructure:"connect_timeout"`
	AllowLocalInsecure bool          `mapstructure:"allow_local_insecure"`
}

func normalizeCanonical(stateDir string, config CanonicalConfig) (CanonicalConfig, error) {
	config.Backend = strings.ToLower(strings.TrimSpace(config.Backend))
	if config.Backend == "" {
		config.Backend = "sqlite"
	}

	pg, err := normalizeCanonicalPostgres(config.Postgres)
	if err != nil {
		return CanonicalConfig{}, err
	}

	config.Postgres = pg

	switch config.Backend {
	case "sqlite":
		defaults := CanonicalPostgresConfig{DSNEnv: defaultPostgresDSNEnv,
			ConnectTimeout: defaultPostgresConnectTimeout}
		if config.Postgres != defaults {
			return CanonicalConfig{}, ErrInvalidCanonicalConfig
		}

		config.SQLite, err = normalizeCanonicalSQLite(stateDir, config.SQLite)
		if err != nil {
			return CanonicalConfig{}, err
		}
	case "postgres":
		if config.SQLite.Path != "" || config.SQLite.BusyTimeout != 0 {
			return CanonicalConfig{}, ErrInvalidCanonicalConfig
		}
	default:
		return CanonicalConfig{}, ErrInvalidCanonicalConfig
	}

	return config, nil
}

func normalizeCanonicalPostgres(config CanonicalPostgresConfig) (CanonicalPostgresConfig, error) {
	config.DSNEnv = strings.TrimSpace(config.DSNEnv)
	if config.DSNEnv == "" {
		config.DSNEnv = defaultPostgresDSNEnv
	}

	if !validEmbeddingEnvironmentName(config.DSNEnv) || config.ConnectTimeout < 0 ||
		config.ConnectTimeout > maxPostgresConnectTimeout {
		return CanonicalPostgresConfig{}, ErrInvalidCanonicalConfig
	}

	if config.ConnectTimeout == 0 {
		config.ConnectTimeout = defaultPostgresConnectTimeout
	}

	return config, nil
}

func normalizeCanonicalSQLite(stateDir string, config CanonicalSQLiteConfig) (CanonicalSQLiteConfig, error) {
	path := strings.TrimSpace(config.Path)
	if path == "" {
		path = filepath.Join(stateDir, "canonical", "canonical.sqlite")
	}

	if path == ":memory:" || strings.HasPrefix(path, "file:") {
		return CanonicalSQLiteConfig{}, ErrInvalidCanonicalConfig
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return CanonicalSQLiteConfig{}, ErrInvalidCanonicalConfig
	}

	config.Path = filepath.Clean(absolute)
	if config.BusyTimeout == 0 {
		config.BusyTimeout = time.Second
	}

	if err := (sqlite.Config{Path: config.Path, BusyTimeout: config.BusyTimeout}).Validate(); err != nil {
		return CanonicalSQLiteConfig{}, ErrInvalidCanonicalConfig
	}

	if err := validateCanonicalPath(stateDir, config.Path); err != nil {
		return CanonicalSQLiteConfig{}, err
	}

	return config, nil
}

// resolveCanonical is called once by New, after structural normalization and
// before runtime construction. The returned private runtime config redacts DSN
// formatting; neither normalized Config nor Host.config retains the secret.
func (config Config) resolveCanonical(ctx context.Context) (standaloneruntime.CanonicalConfig, error) {
	if err := ctx.Err(); err != nil {
		return standaloneruntime.CanonicalConfig{}, err
	}

	resolved := standaloneRuntimeConfig(config).Canonical
	if config.Canonical.Backend != "postgres" {
		return resolved, nil
	}

	secret, ok := os.LookupEnv(config.Canonical.Postgres.DSNEnv)
	if !ok || strings.TrimSpace(secret) == "" || strings.ContainsRune(secret, '\x00') {
		return standaloneruntime.CanonicalConfig{}, ErrInvalidCanonicalConfig
	}

	resolved.Postgres = postgres.Config{DSN: secret, ConnectTimeout: config.Canonical.Postgres.ConnectTimeout,
		AllowLocalInsecure: config.Canonical.Postgres.AllowLocalInsecure}
	if err := resolved.Postgres.Validate(); err != nil {
		return standaloneruntime.CanonicalConfig{}, ErrInvalidCanonicalConfig
	}

	if err := ctx.Err(); err != nil {
		return standaloneruntime.CanonicalConfig{}, err
	}

	return resolved, nil
}
