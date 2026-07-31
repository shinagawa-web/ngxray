package config

import (
	"github.com/BurntSushi/toml"
)

type Config struct {
	Report  ReportConfig  `toml:"report"`
	Workers WorkersConfig `toml:"workers"`
}

type ReportConfig struct {
	Days int `toml:"days"` // must be > 0
}

type WorkersConfig struct {
	Enabled  bool   `toml:"enabled"`
	PIDFile  string `toml:"pid_file"`
	Interval int    `toml:"interval"` // seconds
	Output   string `toml:"output"`
}

func defaults() Config {
	return Config{
		Report: ReportConfig{
			Days: 1,
		},
		Workers: WorkersConfig{
			Enabled:  true,
			PIDFile:  "/var/run/nginx.pid",
			Interval: 60,
			Output:   "/var/log/ngxray/workers.ndjson",
		},
	}
}

func Load(path string) (Config, error) {
	cfg := defaults()
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
