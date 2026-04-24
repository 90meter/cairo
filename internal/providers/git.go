package providers

import (
	"os/exec"
	"strings"
)

// GitProvider contributes git context (current branch and working-tree status)
// on each call. It does a lightweight check on every invocation so the
// system prompt always reflects the current state of the repository.
//
// If cwd is not inside a git repo, or git is not installed, it returns "".
type GitProvider struct{}

// NewGitProvider constructs a GitProvider. No startup detection — git is
// queried fresh on every Context call.
func NewGitProvider() *GitProvider { return &GitProvider{} }

func (p *GitProvider) Name() string { return "git" }

// Context runs `git rev-parse --abbrev-ref HEAD` and `git status --short` in
// cwd. Returns a brief summary, or "" if cwd is not a git repository or git
// is not available.
func (p *GitProvider) Context(cwd string) string {
	if cwd == "" {
		return ""
	}

	// Quick check: are we inside a git repo at all?
	check := exec.Command("git", "rev-parse", "--git-dir")
	check.Dir = cwd
	if err := check.Run(); err != nil {
		return ""
	}

	// Current branch
	branchOut, err := runGit(cwd, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(branchOut)

	// Short status — blank lines mean clean
	statusOut, _ := runGit(cwd, "status", "--short")
	statusOut = strings.TrimSpace(statusOut)

	var b strings.Builder
	b.WriteString("Git repository: branch=")
	b.WriteString(branch)
	if statusOut != "" {
		b.WriteString(", working tree has uncommitted changes")
	} else {
		b.WriteString(", working tree clean")
	}
	b.WriteByte('\n')
	return b.String()
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}
