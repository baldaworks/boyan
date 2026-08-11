package standaloneruntime

import (
	"fmt"
	"strings"
)

type standaloneBuildInputs struct {
	layout     stateLayout
	workingDir string
}

func prepareStandaloneBuild(cfg Config) (standaloneBuildInputs, error) {
	layout, err := buildStateLayout(cfg.StateDir)
	if err != nil {
		return standaloneBuildInputs{}, err
	}
	if strings.TrimSpace(cfg.Derivation.Provider) == "" {
		return standaloneBuildInputs{}, fmt.Errorf("Boyan derivation provider is required; configure derivation.provider")
	}
	workingDir := strings.TrimSpace(cfg.Derivation.WorkingDir)
	if workingDir == "" {
		workingDir = "."
	}
	if err := ensureStandaloneState(layout); err != nil {
		return standaloneBuildInputs{}, err
	}
	return standaloneBuildInputs{
		layout:     layout,
		workingDir: workingDir,
	}, nil
}
