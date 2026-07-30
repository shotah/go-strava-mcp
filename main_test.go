package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestBinary builds the binary once for all tests in this file.
// Tests that need the binary call buildTestBinary(t).
func buildTestBinary(t *testing.T) string {
	t.Helper()
	name := "strava-mcp-test"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binPath := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to build test binary: %v", err)
	}
	return binPath
}

func TestCheckUpdateFlag_DevVersion(t *testing.T) {
	// Default build has Version="dev" (no ldflags override).
	bin := buildTestBinary(t)

	cmd := exec.Command(bin, "--check-update")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// --check-update with dev version should exit 0.
		t.Fatalf("--check-update exited with error: %v\noutput: %s", err, output)
	}

	if got := string(output); !containsSubstr(got, "dev build") {
		t.Errorf("expected 'dev build' in output, got: %s", got)
	}
}

func TestVersionFlag_StillWorks(t *testing.T) {
	bin := buildTestBinary(t)

	cmd := exec.Command(bin, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--version exited with error: %v\noutput: %s", err, output)
	}

	got := string(output)
	if !containsSubstr(got, "strava-mcp") {
		t.Errorf("expected 'strava-mcp' in --version output, got: %s", got)
	}
	if !containsSubstr(got, "dev") {
		t.Errorf("expected 'dev' in --version output, got: %s", got)
	}
}

func TestCheckUpdateFlag_ParsesCorrectly(t *testing.T) {
	// Verify --check-update and --debug can coexist without conflict.
	bin := buildTestBinary(t)

	cmd := exec.Command(bin, "--debug", "--check-update")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--debug --check-update exited with error: %v\noutput: %s", err, output)
	}

	if got := string(output); !containsSubstr(got, "dev build") {
		t.Errorf("expected 'dev build' in output, got: %s", got)
	}
}

func TestCacheDir_ReturnsValidPath(t *testing.T) {
	dir := cacheDir()
	if dir == "" {
		t.Fatal("cacheDir() returned empty string")
	}
	if !containsSubstr(dir, ".strava") {
		t.Errorf("cacheDir() = %q, expected path containing '.strava'", dir)
	}
}

// containsSubstr is a simple helper for readable test assertions.
func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
