package standaloneruntime

import (
	"fmt"
	"os"
	"strings"

	"github.com/rs/zerolog"
)

func newLogger(cfg Config) (zerolog.Logger, error) {
	level := zerolog.InfoLevel

	if strings.TrimSpace(cfg.Log.Level) != "" {
		parsed, err := zerolog.ParseLevel(strings.TrimSpace(cfg.Log.Level))
		if err != nil {
			return zerolog.Logger{}, fmt.Errorf("parse Boyan log level: %w", err)
		}

		level = parsed
	}

	logger := zerolog.New(os.Stderr).With().Timestamp().Str("service", "boyan").Logger().Level(level)

	return logger, nil
}
