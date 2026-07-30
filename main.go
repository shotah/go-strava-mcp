package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/pkg/browser"

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

const (
	checkUpdateTimeout = 10 * time.Second
	selfUpdateTimeout  = 120 * time.Second
	bgCheckTimeout     = 5 * time.Second
	updateCooldown     = 24 * time.Hour
)

// errNoHomeDir is returned when the update cache directory cannot be resolved.
var errNoHomeDir = errors.New("cannot determine home directory")

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

// cliOptions holds the parsed command line.
type cliOptions struct {
	debug       bool
	showVersion bool
	checkUpdate bool
	doUpdate    bool
	forceUpdate bool
	positional  []string
}

// parseCLI parses args manually -- no CLI framework needed for 2 modes + 5 flags.
func parseCLI(args []string) cliOptions {
	var opts cliOptions
	for _, arg := range args {
		switch arg {
		case "--debug":
			opts.debug = true
		case "--version":
			opts.showVersion = true
		case "--check-update":
			opts.checkUpdate = true
		case "--update":
			opts.doUpdate = true
		case "--force":
			opts.forceUpdate = true
		default:
			opts.positional = append(opts.positional, arg)
		}
	}
	return opts
}

// run executes the CLI and returns the process exit code. All diagnostics go to
// w (stderr in production) so stdout stays reserved for MCP JSON-RPC.
func run(args []string, w io.Writer) int {
	// Safety net: redirect standard log and pkg/browser to stderr.
	// This prevents any accidental stdout writes that would corrupt MCP JSON-RPC.
	log.SetOutput(w)
	browser.Stdout = w
	browser.Stderr = w

	opts := parseCLI(args)

	switch {
	case opts.showVersion:
		fmt.Fprintf(w, "strava-mcp %s (%s) built %s\n", Version, Commit, Date)
		return 0
	case opts.checkUpdate:
		return report(w, runCheckUpdate(w))
	case opts.doUpdate:
		return report(w, runSelfUpdate(w, opts.forceUpdate))
	}

	// Configure slog for structured logging to stderr.
	level := slog.LevelInfo
	if opts.debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})))

	// Subcommand dispatch; default is the MCP server.
	if len(opts.positional) > 0 && opts.positional[0] == "auth" {
		return report(w, runAuth())
	}
	return report(w, runServer())
}

// report prints err (if any) and maps it to an exit code.
func report(w io.Writer, err error) int {
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return 1
	}
	return 0
}

// cacheDir returns the ~/.strava/ directory path for the update cache.
func cacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".strava")
}

// newChecker builds an update checker backed by the ~/.strava cache. It is a
// variable so tests can substitute a checker aimed at a local server.
var newChecker = defaultChecker

// defaultChecker returns errNoHomeDir when the cache directory cannot be resolved.
func defaultChecker(logger *slog.Logger) (*update.Checker, error) {
	dir := cacheDir()
	if dir == "" {
		return nil, errNoHomeDir
	}
	return update.NewChecker(Version, update.NewCache(dir), logger), nil
}

// quietLogger logs warnings and above to w; used by the one-shot CLI modes.
func quietLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// runCheckUpdate implements --check-update.
func runCheckUpdate(w io.Writer) error {
	checker, err := newChecker(quietLogger(w))
	if err != nil {
		return err
	}
	if checker.IsDev() {
		fmt.Fprintln(w, "strava-mcp dev build - version check not available")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), checkUpdateTimeout)
	defer cancel()

	result, err := checker.Check(ctx)
	if err != nil {
		return fmt.Errorf("checking for updates: %w", err)
	}
	fmt.Fprintln(w, checker.FormatCheckOutput(result))
	return nil
}

// runSelfUpdate implements --update. force skips the Homebrew guard.
func runSelfUpdate(w io.Writer, force bool) error {
	logger := quietLogger(w)
	checker, err := newChecker(logger)
	if err != nil {
		return err
	}
	if checker.IsDev() {
		fmt.Fprintln(w, "strava-mcp dev build - update not available")
		return nil
	}

	binaryPath, err := resolveBinaryPath()
	if err != nil {
		return err
	}

	// Homebrew detection - warn but allow, --force skips the warning.
	if update.IsHomebrew(binaryPath) && !force {
		fmt.Fprintln(w, "Installed via Homebrew. Recommended: brew upgrade strava-mcp")
		return errors.New("refusing to replace a Homebrew-managed binary; to update anyway run: strava-mcp --update --force")
	}

	// Permission pre-check before downloading anything.
	if err := update.CheckWritePermission(binaryPath); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), selfUpdateTimeout)
	defer cancel()

	progress := func(msg string) {
		fmt.Fprintln(w, msg)
	}
	return update.NewUpdater(checker, logger).Update(ctx, binaryPath, progress)
}

// resolveBinaryPath returns the running executable with symlinks resolved.
func resolveBinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot determine binary path: %w", err)
	}
	// Not every install is symlinked; fall back to the raw path.
	if resolved, evalErr := filepath.EvalSymlinks(exe); evalErr == nil {
		return resolved, nil
	}
	return exe, nil
}

func runServer() error {
	s, err := buildServer()
	if err != nil {
		return err
	}
	slog.Info("starting MCP server", "name", "strava", "version", Version)

	startBackgroundUpdateCheck()

	if err := mcpserver.ServeStdio(s); err != nil {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

// buildServer loads configuration and wires up the MCP server.
func buildServer() (*mcpserver.MCPServer, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("configuration error: %w", err)
	}
	store := auth.NewFileTokenStore(cfg.TokenPath)
	client := strava.NewClient(cfg, store, slog.Default())
	return server.New(Version, client, serverOptions()), nil
}

// serverOptions builds the optional update dependencies for the MCP tools.
// Returns nil for dev builds or when the cache directory is unavailable, in
// which case the update tools are simply not registered.
func serverOptions() *server.Options {
	checker, err := newChecker(slog.Default())
	if err != nil || checker.IsDev() {
		return nil
	}
	return &server.Options{
		Checker: checker,
		Updater: update.NewUpdater(checker, slog.Default()),
	}
}

// startBackgroundUpdateCheck launches a non-blocking, silent-fail version check.
func startBackgroundUpdateCheck() {
	if os.Getenv("STRAVA_MCP_NO_UPDATE_CHECK") != "" {
		return
	}
	checker, err := newChecker(slog.Default())
	if err != nil || checker.IsDev() {
		return
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Debug("update check panicked", "err", r)
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), bgCheckTimeout)
		defer cancel()

		result, err := checker.CheckWithCooldown(ctx, updateCooldown)
		if err != nil {
			slog.Debug("update check failed", "err", err)
			return
		}
		if msg := checker.FormatNotification(result); msg != "" {
			fmt.Fprintln(os.Stderr, msg)
		}
	}()
}

func runAuth() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}
	store := auth.NewFileTokenStore(cfg.TokenPath)
	if err := auth.RunOAuthFlow(cfg, store, slog.Default()); err != nil {
		return fmt.Errorf("auth failed: %w", err)
	}
	return nil
}
