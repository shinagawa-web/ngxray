package config

import (
	"testing"
)

func TestLoadFullConfig(t *testing.T) {
	cfg, err := Load("testdata/full.toml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Report.Days != 14 {
		t.Errorf("report.days: got %d, want 14", cfg.Report.Days)
	}
	w := cfg.Workers
	if !w.Enabled {
		t.Error("enabled: got false, want true")
	}
	if w.PIDFile != "/run/nginx.pid" {
		t.Errorf("pid_file: got %q, want /run/nginx.pid", w.PIDFile)
	}
	if w.Interval != 30 {
		t.Errorf("interval: got %d, want 30", w.Interval)
	}
	if w.Output != "/tmp/workers.ndjson" {
		t.Errorf("output: got %q, want /tmp/workers.ndjson", w.Output)
	}
}


func TestLoadPartialConfigUsesDefaults(t *testing.T) {
	cfg, err := Load("testdata/partial.toml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Report.Days != 1 {
		t.Errorf("report.days: got %d, want default 1", cfg.Report.Days)
	}
	w := cfg.Workers
	// interval is overridden
	if w.Interval != 120 {
		t.Errorf("interval: got %d, want 120", w.Interval)
	}
	// everything else should be the default
	if !w.Enabled {
		t.Error("enabled: got false, want default true")
	}
	if w.PIDFile != "/var/run/nginx.pid" {
		t.Errorf("pid_file: got %q, want default /var/run/nginx.pid", w.PIDFile)
	}
	if w.Output != "/var/log/ngxray/workers.ndjson" {
		t.Errorf("output: got %q, want default /var/log/ngxray/workers.ndjson", w.Output)
	}
}

func TestLoadMissingFileReturnsError(t *testing.T) {
	_, err := Load("testdata/does_not_exist.toml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestLoadInvalidTOMLReturnsError(t *testing.T) {
	_, err := Load("testdata/invalid.toml")
	if err == nil {
		t.Error("expected error for invalid TOML, got nil")
	}
}
