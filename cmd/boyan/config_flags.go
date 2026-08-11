package main

import (
	"fmt"

	standalonecfg "github.com/baldaworks/boyan/internal/standalonecfg"
	"github.com/spf13/pflag"
)

func bindServeFlags(flags *pflag.FlagSet) {
	flags.String(configFlagStateDir, "", "explicit Boyan state root")
	flags.String(configFlagListen, "", "loopback listener address for HTTP ingest and MCP")
}

func applyConfigFlags(cfg standalonecfg.Config, flags *pflag.FlagSet) (standalonecfg.Config, error) {
	if flags == nil {
		return cfg, nil
	}
	if flags.Changed(configFlagStateDir) {
		stateDir, err := flags.GetString(configFlagStateDir)
		if err != nil {
			return standalonecfg.Config{}, fmt.Errorf("read Boyan state-dir flag: %w", err)
		}
		cfg.StateDir = stateDir
	}
	if flags.Changed(configFlagListen) {
		listen, err := flags.GetString(configFlagListen)
		if err != nil {
			return standalonecfg.Config{}, fmt.Errorf("read Boyan listen flag: %w", err)
		}
		cfg.HTTP.Listen = listen
	}
	return cfg, nil
}
