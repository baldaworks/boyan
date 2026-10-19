package standalonecfg

import (
	boyan "github.com/baldaworks/boyan/pkg/boyan"
	"github.com/spf13/viper"
)

func defaultApplicationConfig() Config {
	return boyan.DefaultConfig()
}

func setApplicationDefaults(loader *viper.Viper, cfg Config) {
	loader.SetDefault("state_dir", cfg.StateDir)
	loader.SetDefault("canonical.backend", cfg.Canonical.Backend)
	loader.SetDefault("canonical.sqlite.path", cfg.Canonical.SQLite.Path)
	loader.SetDefault("canonical.sqlite.busy_timeout", cfg.Canonical.SQLite.BusyTimeout)
	loader.SetDefault("canonical.postgres.dsn_env", cfg.Canonical.Postgres.DSNEnv)
	loader.SetDefault("canonical.postgres.connect_timeout", cfg.Canonical.Postgres.ConnectTimeout)
	loader.SetDefault("canonical.postgres.allow_local_insecure", cfg.Canonical.Postgres.AllowLocalInsecure)
	loader.SetDefault("log.level", cfg.Log.Level)
	loader.SetDefault("http.listen", cfg.HTTP.Listen)
	loader.SetDefault("http.request_timeout", cfg.HTTP.RequestTimeout)
	loader.SetDefault("http.shutdown_timeout", cfg.HTTP.ShutdownTimeout)
	loader.SetDefault("http.max_body_bytes", cfg.HTTP.MaxBodyBytes)
	loader.SetDefault("derivation.provider", cfg.Derivation.Provider)
	loader.SetDefault("derivation.working_dir", cfg.Derivation.WorkingDir)
	loader.SetDefault("derivation.timeout", cfg.Derivation.Timeout)
	loader.SetDefault("derivation.max_output_bytes", cfg.Derivation.MaxOutputBytes)
	loader.SetDefault("maintenance.value_log_gc_interval", cfg.Maintenance.ValueLogGCInterval)
	loader.SetDefault("maintenance.discard_ratio", cfg.Maintenance.DiscardRatio)
	loader.SetDefault("embeddings.enabled", cfg.Embeddings.Enabled)
	loader.SetDefault("embeddings.endpoint", cfg.Embeddings.Endpoint)
	loader.SetDefault("embeddings.model", cfg.Embeddings.Model)
	loader.SetDefault("embeddings.revision", cfg.Embeddings.Revision)
	loader.SetDefault("embeddings.dimensions", cfg.Embeddings.Dimensions)
	loader.SetDefault("embeddings.query_prefix", cfg.Embeddings.QueryPrefix)
	loader.SetDefault("embeddings.passage_prefix", cfg.Embeddings.PassagePrefix)
	loader.SetDefault("embeddings.api_key_env", cfg.Embeddings.APIKeyEnv)
	loader.SetDefault("embeddings.failure_policy", cfg.Embeddings.FailurePolicy)
}
