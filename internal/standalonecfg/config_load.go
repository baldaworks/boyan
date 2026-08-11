package standalonecfg

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

func LoadConfig(configPath string) (Config, error) {
	cfg := defaultApplicationConfig()
	loader := viper.New()
	loader.SetConfigType("yaml")
	loader.SetEnvPrefix("BOYAN")
	loader.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	loader.AutomaticEnv()

	setApplicationDefaults(loader, cfg)
	if trimmed := strings.TrimSpace(configPath); trimmed != "" {
		loader.SetConfigFile(trimmed)
		if err := loader.ReadInConfig(); err != nil {
			return Config{}, fmt.Errorf("read Boyan config: %w", err)
		}
	}
	if err := loader.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode Boyan config: %w", err)
	}
	return cfg, nil
}
