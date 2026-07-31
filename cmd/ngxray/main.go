package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/shinagawa-web/ngxray/internal/config"
	"github.com/shinagawa-web/ngxray/internal/workers"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "collect":
		runCollect(os.Args[2:])
	case "report":
		runReport(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage:
  ngxray collect --config <path>  collect metrics and write NDJSON logs
  ngxray report  --config <path>  report on all enabled features

`)
}

func runCollect(args []string) {
	fs := flag.NewFlagSet("collect", flag.ExitOnError)
	cfgPath := fs.String("config", defaultConfigPath(), "path to config file")
	fs.Parse(args)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	if !cfg.Workers.Enabled {
		log.Println("workers collection disabled")
		os.Exit(0)
	}

	out, err := openAppend(cfg.Workers.Output)
	if err != nil {
		log.Fatalf("open output %s: %v", cfg.Workers.Output, err)
	}
	defer out.Close()

	c := &workers.Collector{
		ProcRoot: "/proc",
		PIDFile:  cfg.Workers.PIDFile,
		Out:      out,
	}

	interval := time.Duration(cfg.Workers.Interval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	// Collect once immediately, then on each tick
	if err := c.Collect(); err != nil {
		log.Printf("collect: %v", err)
	}
	for {
		select {
		case <-ticker.C:
			if err := c.Collect(); err != nil {
				log.Printf("collect: %v", err)
			}
		case <-sig:
			return
		}
	}
}

func runReport(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	cfgPath := fs.String("config", defaultConfigPath(), "path to config file")
	fs.Parse(args)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	if cfg.Report.Days == 0 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -cfg.Report.Days)

	if cfg.Workers.Enabled {
		f, err := os.Open(cfg.Workers.Output)
		if err != nil {
			log.Fatalf("open %s: %v", cfg.Workers.Output, err)
		}
		defer f.Close()
		fmt.Println("=== worker generations ===")
		if err := workers.Analyze(f, cutoff, os.Stdout); err != nil {
			log.Fatalf("workers report: %v", err)
		}
	}
}

// defaultConfigPath returns ngxray.toml in the same directory as the executable.
func defaultConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "ngxray.toml"
	}
	return filepath.Join(filepath.Dir(exe), "ngxray.toml")
}

// openAppend opens a file for appending, creating parent directories as needed.
func openAppend(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
}
