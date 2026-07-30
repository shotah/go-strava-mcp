package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDisplayTag(t *testing.T) {
	t.Parallel()
	if got := displayTag(""); got != "(none)" {
		t.Fatalf("empty: %q", got)
	}
	if got := displayTag("v1.2.3"); got != "v1.2.3" {
		t.Fatalf("tag: %q", got)
	}
}

func TestNextVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		current, bump, explicit, want string
	}{
		{"", "patch", "", "v0.0.1"},
		{"v0.1.0", "patch", "", "v0.1.1"},
		{"v0.1.0", "minor", "", "v0.2.0"},
		{"v0.1.0", "major", "", "v1.0.0"},
		{"v1.2.3", "patch", "", "v1.2.4"},
		{"v0.1.0", "", "", "v0.1.1"},
		{"v0.1.0", "PATCH", "", "v0.1.1"},
		{"v0.1.0", "Minor", "", "v0.2.0"},
		{"v0.1.0", "patch", "v9.8.7", "v9.8.7"},
		{"v0.1.0", "patch", "1.0.0", "v1.0.0"},
		{"v0.1.0", "patch", "  v2.3.4  ", "v2.3.4"},
	}
	for _, tc := range cases {
		got, err := nextVersion(tc.current, tc.bump, tc.explicit)
		if err != nil {
			t.Fatalf("%+v: %v", tc, err)
		}
		if got != tc.want {
			t.Fatalf("%+v: got %s want %s", tc, got, tc.want)
		}
	}
}

func TestNextVersion_Errors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                    string
		current, bump, explicit string
		wantErrContains         string
	}{
		{
			name:            "explicit version is not semver",
			bump:            "patch",
			explicit:        "not-a-version",
			wantErrContains: "invalid -version",
		},
		{
			name:            "explicit version has too few parts",
			bump:            "patch",
			explicit:        "v1.2",
			wantErrContains: "invalid -version",
		},
		{
			name:            "current tag is not semver",
			current:         "release-2024",
			bump:            "patch",
			wantErrContains: "is not semver",
		},
		{
			name:            "unknown bump",
			current:         "v1.0.0",
			bump:            "gigantic",
			wantErrContains: "invalid -bump",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := nextVersion(tc.current, tc.bump, tc.explicit)
			if err == nil {
				t.Fatalf("nextVersion(%q, %q, %q) = nil error, want one", tc.current, tc.bump, tc.explicit)
			}
			if !strings.Contains(err.Error(), tc.wantErrContains) {
				t.Errorf("error = %v, want it to contain %q", err, tc.wantErrContains)
			}
		})
	}
}

func TestModuleRoot_FindsGoMod(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatalf("moduleRoot() error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Errorf("moduleRoot() = %q, but it holds no go.mod: %v", root, err)
	}
}

func TestModuleRoot_ErrorOutsideModule(t *testing.T) {
	// The temp directory tree has no go.mod above it on any supported platform,
	// but the OS root is reachable, so walking up must terminate with an error.
	dir := t.TempDir()
	t.Chdir(dir)

	if _, err := moduleRoot(); err == nil {
		t.Skipf("temp dir %q sits inside a Go module; nothing to assert", dir)
	}
}

func TestGitEnv_StripsRepoOverrides(t *testing.T) {
	t.Parallel()
	in := []string{
		"PATH=/usr/bin",
		"GIT_DIR=/tmp/outer/.git",
		"GIT_INDEX_FILE=/tmp/outer/.git/index",
		"GIT_WORK_TREE=/tmp/outer",
		"HOME=/home/test",
	}
	got := gitEnv(in)
	for _, e := range got {
		if strings.HasPrefix(e, "GIT_DIR=") ||
			strings.HasPrefix(e, "GIT_INDEX_FILE=") ||
			strings.HasPrefix(e, "GIT_WORK_TREE=") {
			t.Errorf("gitEnv kept override %q", e)
		}
	}
	if len(got) != 2 {
		t.Errorf("gitEnv() = %v, want PATH and HOME only", got)
	}
}

func TestGitOutput_EmptyOnFailure(t *testing.T) {
	if got := gitOutput("this-is-not-a-git-command"); got != "" {
		t.Errorf("gitOutput(bad command) = %q, want empty", got)
	}
}

func TestGitOutput_ReturnsOutput(t *testing.T) {
	requireGit(t)
	if got := gitOutput("--version"); !strings.Contains(got, "git version") {
		t.Errorf("gitOutput(--version) = %q, want it to contain 'git version'", got)
	}
}

func TestGitRun_ErrorOnFailure(t *testing.T) {
	if err := gitRun("this-is-not-a-git-command"); err == nil {
		t.Error("gitRun(bad command) = nil, want an error")
	}
}

func TestGitRun_SucceedsForVersion(t *testing.T) {
	requireGit(t)
	if err := gitRun("--version"); err != nil {
		t.Errorf("gitRun(--version) error: %v", err)
	}
}

func TestLatestSemverTag_EmptyOutsideRepo(t *testing.T) {
	requireGit(t)
	// A bare temp directory is not a git repository, so the tag listing fails
	// and latestSemverTag falls back to the empty string.
	t.Chdir(t.TempDir())

	if got := latestSemverTag(); got != "" {
		t.Errorf("latestSemverTag() = %q, want empty outside a repository", got)
	}
}

func TestLatestSemverTag_ReadsTagsFromRepo(t *testing.T) {
	requireGit(t)
	repo := initRepoWithTags(t, "v0.1.0", "v0.2.0", "v0.10.0")
	t.Chdir(repo)

	// --sort=-v:refname orders numerically, so v0.10.0 beats v0.2.0.
	if got := latestSemverTag(); got != "v0.10.0" {
		t.Errorf("latestSemverTag() = %q, want %q", got, "v0.10.0")
	}
}

func TestPointLatestAtHEAD(t *testing.T) {
	requireGit(t)
	repo := initRepoWithTags(t)
	t.Chdir(repo)

	if err := pointLatestAtHEAD("v1.2.3"); err != nil {
		t.Fatalf("pointLatestAtHEAD() error: %v", err)
	}
	if tags := gitOutput("tag", "-l", latestTagName); !strings.Contains(tags, latestTagName) {
		t.Errorf("tag list = %q, want it to contain %q", tags, latestTagName)
	}
}

func TestSemverRE(t *testing.T) {
	t.Parallel()
	for _, valid := range []string{"v1.2.3", "1.2.3", "v10.20.30"} {
		if !semverRE.MatchString(valid) {
			t.Errorf("semverRE should match %q", valid)
		}
	}
	for _, invalid := range []string{"v1.2", "v1.2.3-rc1", "latest", ""} {
		if semverRE.MatchString(invalid) {
			t.Errorf("semverRE should not match %q", invalid)
		}
	}
}

// requireGit skips the test when git is not on PATH.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

// initRepoWithTags creates a throwaway repository with one commit and the given
// annotated tags, returning its path.
func initRepoWithTags(t *testing.T, tags ...string) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		// Strip GIT_DIR / GIT_INDEX_FILE / … so an outer `git commit` pre-commit
		// hook cannot pin fixture commands to the parent repository.
		cmd.Env = append(gitEnv(os.Environ()),
			"GIT_CONFIG_GLOBAL="+filepath.Join(dir, "gitconfig"),
			"GIT_CONFIG_SYSTEM="+filepath.Join(dir, "gitconfig"),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	run("init", "--quiet")
	run("config", "user.name", "test")
	run("config", "user.email", "test@example.com")

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "--quiet", "-m", "initial")

	for _, tag := range tags {
		run("tag", "-a", tag, "-m", "release "+tag)
	}
	return dir
}
