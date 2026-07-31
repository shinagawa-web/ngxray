package main

import (
	"context"
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

// Injectable for testing.
var (
	osExecutable = os.Executable
	osExit       = os.Exit
	logFatal     = log.Fatal
	logFatalf    = log.Fatalf
)

func main() {
	ctx := withSignalCancel(context.Background())

	if len(os.Args) < 2 {
		usage()
		osExit(1)
		return
	}
	switch os.Args[1] {
	case "collect":
		runCollect(ctx, os.Args[2:])
	case "report":
		runReport(os.Args[2:])
	default:
		usage()
		osExit(1)
	}
}

// withSignalCancel returns a context that is cancelled on SIGINT or SIGTERM.
func withSignalCancel(parent context.Context) context.Context {
	ctx, cancel := context.WithCancel(parent)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		cancel()
	}()
	return ctx
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage:
  ngxray collect --config <path>  collect metrics and write NDJSON logs
  ngxray report  --config <path>  report on all enabled features

`)
}

func runCollect(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("collect", flag.ExitOnError)
	cfgPath := fs.String("config", defaultConfigPath(), "path to config file")
	fs.Parse(args)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		logFatalf("load config: %v", err)
		return
	}

	if !cfg.Workers.Enabled && !cfg.FD.Enabled {
		log.Println("all collection disabled")
		return
	}

	var wg sync.WaitGroup

	if cfg.Workers.Enabled {
		if cfg.Workers.Interval <= 0 {
			logFatalf("workers.interval must be > 0, got %d", cfg.Workers.Interval)
			return
		}
		out, err := openAppend(cfg.Workers.Output)
		if err != nil {
			logFatalf("open output %s: %v", cfg.Workers.Output, err)
			return
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
			runCollector(ctx, "workers", c.Collect, interval)
		}()
	}

	if cfg.FD.Enabled {
		if cfg.FD.Interval <= 0 {
			logFatalf("fd.interval must be > 0, got %d", cfg.FD.Interval)
			return
		}
		out, err := openAppend(cfg.FD.Output)
		if err != nil {
			logFatalf("open output %s: %v", cfg.FD.Output, err)
			return
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
			runCollector(ctx, "fd", c.Collect, interval)
		}()
	}

	wg.Wait()
}

// runCollector runs collectFn once immediately, then on each tick, until ctx is cancelled.
func runCollector(ctx context.Context, name string, collectFn func() error, interval time.Duration) {
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
		case <-ctx.Done():
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
		logFatalf("load config: %v", err)
		return
	}

	if cfg.Report.Days == 0 {
		logFatal("report.days must be greater than 0: set the number of days to look back in config")
		return
	}
	cutoff := time.Now().AddDate(0, 0, -cfg.Report.Days)

	if cfg.Workers.Enabled {
		f, err := os.Open(cfg.Workers.Output)
		if err != nil {
			logFatalf("open %s: %v", cfg.Workers.Output, err)
			return
		}
		defer f.Close()
		fmt.Println("=== worker generations ===")
		skipped, err := workers.Analyze(f, cutoff, os.Stdout)
		if err != nil {
			logFatalf("workers report: %v", err)
			return
		}
		if skipped > 0 {
			log.Printf("workers: skipped %d corrupt line(s)", skipped)
		}
	}

	if cfg.FD.Enabled {
		f, err := os.Open(cfg.FD.Output)
		if err != nil {
			logFatalf("open %s: %v", cfg.FD.Output, err)
			return
		}
		defer f.Close()
		fmt.Println("=== FD exhaustion ===")
		skipped, err := fd.Analyze(f, cutoff, os.Stdout)
		if err != nil {
			logFatalf("fd report: %v", err)
			return
		}
		if skipped > 0 {
			log.Printf("fd: skipped %d corrupt line(s)", skipped)
		}
	}
}

// defaultConfigPath returns ngxray.toml in the same directory as the executable.
func defaultConfigPath() string {
	exe, err := osExecutable()
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
