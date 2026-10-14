package standalonecfg

import (
	"bytes"
	"fmt"
	"os"
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

	var providerIDs []string

	if trimmed := strings.TrimSpace(configPath); trimmed != "" {
		loader.SetConfigFile(trimmed)

		data, err := os.ReadFile(trimmed)
		if err != nil {
			return Config{}, fmt.Errorf("read Boyan config: %w", err)
		}

		if err := loader.ReadConfig(bytes.NewReader(data)); err != nil {
			return Config{}, fmt.Errorf("read Boyan config: %w", err)
		}

		if err := validateConfigKeys(data); err != nil {
			return Config{}, err
		}

		providerIDs, err = configuredProviderIDs(data)
		if err != nil {
			return Config{}, err
		}
	}

	if err := bindConfiguredProviderEnvironment(loader, providerIDs); err != nil {
		return Config{}, err
	}

	if err := loader.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode Boyan config: %w", err)
	}

	return cfg, nil
}
