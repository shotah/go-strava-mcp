package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/browser"

	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/shotah/go-strava-mcp/internal/auth"
	"github.com/shotah/go-strava-mcp/internal/config"
	"github.com/shotah/go-strava-mcp/internal/server"
	"github.com/shotah/go-strava-mcp/internal/strava"
	"github.com/shotah/go-strava-mcp/internal/update"
)

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func main() {
	// Safety net: redirect standard log and pkg/browser to stderr.
	// This prevents any accidental stdout writes that would corrupt MCP JSON-RPC.
	log.SetOutput(os.Stderr)
	browser.Stdout = os.Stderr
	browser.Stderr = os.Stderr

	// Parse args manually -- no CLI framework needed for 2 modes + 5 flags.
	debug := false
	showVersion := false
	checkUpdate := false
	doUpdate := false
	forceUpdate := false

	args := os.Args[1:]
	var positional []string
	for _, arg := range args {
		switch arg {
		case "--debug":
			debug = true
		case "--version":
			showVersion = true
		case "--check-update":
			checkUpdate = true
		case "--update":
			doUpdate = true
		case "--force":
			forceUpdate = true
		default:
			positional = append(positional, arg)
		}
	}

	if showVersion {
		fmt.Fprintf(os.Stderr, "strava-mcp %s (%s) built %s\n", Version, Commit, Date)
		os.Exit(0)
	}

	if checkUpdate {
		dir := cacheDir()
		if dir == "" {
			fmt.Fprintf(os.Stderr, "error: cannot determine home directory\n")
			os.Exit(1)
		}
		cache := update.NewCache(dir)
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
		checker := update.NewChecker(Version, cache, logger)
		if checker.IsDev() {
			fmt.Fprintf(os.Stderr, "strava-mcp dev build â€” version check not available\n")
			os.Exit(0)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		result, err := checker.Check(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error checking for updates: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprint(os.Stderr, checker.FormatCheckOutput(result))
		fmt.Fprintln(os.Stderr)
		os.Exit(0)
	}

	if doUpdate {
		dir := cacheDir()
		if dir == "" {
			fmt.Fprintf(os.Stderr, "error: cannot determine home directory\n")
			os.Exit(1)
		}

		cache := update.NewCache(dir)
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
		checker := update.NewChecker(Version, cache, logger)

		if checker.IsDev() {
			fmt.Fprintf(os.Stderr, "strava-mcp dev build â€” update not available\n")
			os.Exit(0)
		}

		// Resolve the actual binary path (follows symlinks).
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: cannot determine binary path: %v\n", err)
			os.Exit(1)
		}
		binaryPath, err := filepath.EvalSymlinks(exe)
		if err != nil {
			binaryPath = exe
		}

		// Homebrew detection â€” warn but allow, --force skips warning.
		if update.IsHomebrew(binaryPath) && !forceUpdate {
			fmt.Fprintf(os.Stderr, "Installed via Homebrew. Recommended: brew upgrade strava-mcp\n")
			fmt.Fprintf(os.Stderr, "To update anyway, run: strava-mcp --update --force\n")
			os.Exit(1)
		}

		// Permission pre-check before downloading anything.
		if err := update.CheckWritePermission(binaryPath); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		// Run the update.
		updater := update.NewUpdater(checker, logger)
		progress := func(msg string) {
			fmt.Fprintf(os.Stderr, "%s\n", msg)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		if err := updater.Update(ctx, binaryPath, progress); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Configure slog for structured logging to stderr.
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	// Subcommand dispatch.
	if len(positional) > 0 && positional[0] == "auth" {
		runAuth()
		return
	}

	// Default: run MCP server.
	runServer(debug)
}

// cacheDir returns the ~/.strava/ directory path for the update cache.
func cacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".strava")
}

func runServer(debug bool) {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration error", "err", err)
		os.Exit(1)
	}
	store := auth.NewFileTokenStore(cfg.TokenPath)
	client := strava.NewClient(cfg, store, slog.Default())

	// Build update dependencies for MCP tools (nil-safe if cacheDir fails).
	var opts *server.Options
	dir := cacheDir()
	if dir != "" && Version != "dev" {
		cache := update.NewCache(dir)
		checker := update.NewChecker(Version, cache, slog.Default())
		updater := update.NewUpdater(checker, slog.Default())
		opts = &server.Options{
			Checker: checker,
			Updater: updater,
		}
	}

	s := server.New(Version, client, opts)
	slog.Info("starting MCP server", "name", "strava-mcp", "version", Version)

	// Launch background version check (non-blocking, silent fail).
	if os.Getenv("STRAVA_MCP_NO_UPDATE_CHECK") == "" && Version != "dev" {
		dir := cacheDir()
		if dir != "" {
			go func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Debug("update check panicked", "err", r)
					}
				}()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				cache := update.NewCache(dir)
				checker := update.NewChecker(Version, cache, slog.Default())
				result, err := checker.CheckWithCooldown(ctx, 24*time.Hour)
				if err != nil {
					slog.Debug("update check failed", "err", err)
					return
				}
				if result != nil && result.UpdateAvailable {
					msg := checker.FormatNotification(result)
					if msg != "" {
						fmt.Fprint(os.Stderr, msg)
						fmt.Fprintln(os.Stderr)
					}
				}
			}()
		}
	}

	if err := mcpserver.ServeStdio(s); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

func runAuth() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration error", "err", err)
		os.Exit(1)
	}
	store := auth.NewFileTokenStore(cfg.TokenPath)
	if err := auth.RunOAuthFlow(cfg, store, slog.Default()); err != nil {
		slog.Error("auth failed", "err", err)
		os.Exit(1)
	}
}
