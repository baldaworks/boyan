package main

import (
	"fmt"
	"strings"
	"time"

	runtimeconfig "github.com/normahq/runtime/v2/appconfig"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type applicationConfig struct {
	StateDir    string                     `mapstructure:"state_dir"`
	Log         logConfig                  `mapstructure:"log"`
	HTTP        httpConfig                 `mapstructure:"http"`
	Derivation  derivationConfig           `mapstructure:"derivation"`
	Maintenance canonicalMaintenanceConfig `mapstructure:"maintenance"`
}

type logConfig struct {
	Level string `mapstructure:"level"`
}

type httpConfig struct {
	Listen          string        `mapstructure:"listen"`
	RequestTimeout  time.Duration `mapstructure:"request_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	MaxBodyBytes    int64         `mapstructure:"max_body_bytes"`
}

type derivationConfig struct {
	Provider       string                      `mapstructure:"provider"`
	WorkingDir     string                      `mapstructure:"working_dir"`
	Timeout        time.Duration               `mapstructure:"timeout"`
	MaxOutputBytes int                         `mapstructure:"max_output_bytes"`
	Runtime        runtimeconfig.RuntimeConfig `mapstructure:"runtime"`
}

type canonicalMaintenanceConfig struct {
	ValueLogGCInterval time.Duration `mapstructure:"value_log_gc_interval"`
	DiscardRatio       float64       `mapstructure:"discard_ratio"`
}

func defaultApplicationConfig() applicationConfig {
	return applicationConfig{
		StateDir: defaultStateDir,
		Log: logConfig{
			Level: "info",
		},
		HTTP: httpConfig{
			Listen:          defaultHTTPListenAddress,
			RequestTimeout:  defaultHTTPRequestTimeout,
			ShutdownTimeout: defaultShutdownTimeout,
			MaxBodyBytes:    defaultHTTPMaxBodyBytes,
		},
		Derivation: derivationConfig{
			WorkingDir:     ".",
			Timeout:        defaultDerivationTimeout,
			MaxOutputBytes: defaultDerivationMaxOutputBytes,
		},
		Maintenance: canonicalMaintenanceConfig{
			ValueLogGCInterval: defaultCanonicalGCInterval,
			DiscardRatio:       defaultCanonicalDiscard,
		},
	}
}

func bindServeFlags(flags *pflag.FlagSet) {
	flags.String(configFlagStateDir, "", "explicit Boyan state root")
	flags.String(configFlagListen, "", "loopback listener address for HTTP ingest and MCP")
}

func loadApplicationConfig(configPath string, flags *pflag.FlagSet) (applicationConfig, error) {
	cfg := defaultApplicationConfig()
	loader := viper.New()
	loader.SetConfigType("yaml")
	loader.SetEnvPrefix("BOYAN")
	loader.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	loader.AutomaticEnv()

	setApplicationDefaults(loader, cfg)
	if flags != nil {
		if err := loader.BindPFlags(flags); err != nil {
			return applicationConfig{}, fmt.Errorf("bind Boyan flags: %w", err)
		}
	}
	if trimmed := strings.TrimSpace(configPath); trimmed != "" {
		loader.SetConfigFile(trimmed)
		if err := loader.ReadInConfig(); err != nil {
			return applicationConfig{}, fmt.Errorf("read Boyan config: %w", err)
		}
	}
	if err := loader.Unmarshal(&cfg); err != nil {
		return applicationConfig{}, fmt.Errorf("decode Boyan config: %w", err)
	}
	if flags != nil {
		if flags.Changed(configFlagStateDir) {
			stateDir, err := flags.GetString(configFlagStateDir)
			if err != nil {
				return applicationConfig{}, fmt.Errorf("read Boyan state-dir flag: %w", err)
			}
			cfg.StateDir = stateDir
		}
		if flags.Changed(configFlagListen) {
			listen, err := flags.GetString(configFlagListen)
			if err != nil {
				return applicationConfig{}, fmt.Errorf("read Boyan listen flag: %w", err)
			}
			cfg.HTTP.Listen = listen
		}
	}
	return cfg, nil
}

func setApplicationDefaults(loader *viper.Viper, cfg applicationConfig) {
	loader.SetDefault("state_dir", cfg.StateDir)
	loader.SetDefault("log.level", cfg.Log.Level)
	loader.SetDefault("http.listen", cfg.HTTP.Listen)
	loader.SetDefault("http.request_timeout", cfg.HTTP.RequestTimeout)
	loader.SetDefault("http.shutdown_timeout", cfg.HTTP.ShutdownTimeout)
	loader.SetDefault("http.max_body_bytes", cfg.HTTP.MaxBodyBytes)
	loader.SetDefault("derivation.working_dir", cfg.Derivation.WorkingDir)
	loader.SetDefault("derivation.timeout", cfg.Derivation.Timeout)
	loader.SetDefault("derivation.max_output_bytes", cfg.Derivation.MaxOutputBytes)
	loader.SetDefault("maintenance.value_log_gc_interval", cfg.Maintenance.ValueLogGCInterval)
	loader.SetDefault("maintenance.discard_ratio", cfg.Maintenance.DiscardRatio)
}
