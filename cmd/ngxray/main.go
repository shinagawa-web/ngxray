package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/shinagawa-web/ngxray/internal/config"
	"github.com/shinagawa-web/ngxray/internal/fd"
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

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	var wg sync.WaitGroup

	if cfg.Workers.Enabled {
		out, err := openAppend(cfg.Workers.Output)
		if err != nil {
			log.Fatalf("open output %s: %v", cfg.Workers.Output, err)
		}
		c := &workers.Collector{
			ProcRoot: "/proc",
			PIDFile:  cfg.Workers.PIDFile,
			Out:      out,
		}
		interval := time.Duration(cfg.Workers.Interval) * time.Second
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer out.Close()
			runCollector("workers", c.Collect, interval, sig)
		}()
	}

	if cfg.FD.Enabled {
		out, err := openAppend(cfg.FD.Output)
		if err != nil {
			log.Fatalf("open output %s: %v", cfg.FD.Output, err)
		}
		c := &fd.Collector{
			ProcRoot: "/proc",
			PIDFile:  cfg.Workers.PIDFile,
			Out:      out,
		}
		interval := time.Duration(cfg.FD.Interval) * time.Second
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer out.Close()
			runCollector("fd", c.Collect, interval, sig)
		}()
	}

	if !cfg.Workers.Enabled && !cfg.FD.Enabled {
		log.Println("all collection disabled")
		return
	}

	wg.Wait()
}

// runCollector runs collectFn once immediately, then on each tick, until sig.
func runCollector(name string, collectFn func() error, interval time.Duration, sig <-chan os.Signal) {
	if err := collectFn(); err != nil {
		log.Printf("%s collect: %v", name, err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := collectFn(); err != nil {
				log.Printf("%s collect: %v", name, err)
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
		log.Fatal("report.days must be greater than 0: set the number of days to look back in config")
	}
	cutoff := time.Now().AddDate(0, 0, -cfg.Report.Days)

	if cfg.Workers.Enabled {
		f, err := os.Open(cfg.Workers.Output)
		if err != nil {
			log.Fatalf("open %s: %v", cfg.Workers.Output, err)
		}
		defer f.Close()
		fmt.Println("=== worker generations ===")
		skipped, err := workers.Analyze(f, cutoff, os.Stdout)
		if err != nil {
			log.Fatalf("workers report: %v", err)
		}
		if skipped > 0 {
			log.Printf("workers: skipped %d corrupt line(s)", skipped)
		}
	}

	if cfg.FD.Enabled {
		f, err := os.Open(cfg.FD.Output)
		if err != nil {
			log.Fatalf("open %s: %v", cfg.FD.Output, err)
		}
		defer f.Close()
		fmt.Println("=== FD exhaustion ===")
		skipped, err := fd.Analyze(f, cutoff, os.Stdout)
		if err != nil {
			log.Fatalf("fd report: %v", err)
		}
		if skipped > 0 {
			log.Printf("fd: skipped %d corrupt line(s)", skipped)
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
