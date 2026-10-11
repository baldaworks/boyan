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
}
