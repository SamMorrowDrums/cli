package mcp

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRoot builds a cobra tree that mirrors the structure (command and
// subcommand names, plus a few value-taking persistent flags) of the real gh
// CLI closely enough to exercise classification and flag-aware resolution.
func fakeRoot() *cobra.Command {
	root := &cobra.Command{Use: "gh"}
	// Persistent, value-taking flags shared across gh commands. These ensure
	// classifyArgs (which relies on cobra's flag-aware command resolution)
	// correctly skips flag values when locating the target command.
	root.PersistentFlags().StringP("repo", "R", "", "")
	root.PersistentFlags().String("json", "", "")
	root.PersistentFlags().IntP("limit", "L", 0, "")
	root.PersistentFlags().BoolP("web", "w", false, "")
	root.PersistentFlags().Bool("yes", false, "")

	leaf := func(name string) *cobra.Command {
		return &cobra.Command{Use: name, Run: func(*cobra.Command, []string) {}}
	}
	group := func(name string, children ...string) *cobra.Command {
		g := &cobra.Command{Use: name}
		for _, c := range children {
			g.AddCommand(leaf(c))
		}
		return g
	}

	root.AddCommand(group("issue", "list", "view", "status", "create", "edit", "comment", "close", "reopen", "delete", "develop", "transfer", "lock", "pin", "unpin"))
	root.AddCommand(group("pr", "list", "view", "diff", "checks", "status", "create", "edit", "merge", "review", "ready", "revert", "close", "checkout", "update-branch", "comment"))

	repo := group("repo", "view", "list", "create", "edit", "clone", "fork", "sync", "rename", "archive", "unarchive", "delete", "credits", "garden")
	repo.AddCommand(group("deploy-key", "add", "list", "delete"))
	root.AddCommand(repo)

	root.AddCommand(group("release", "list", "view", "create", "edit", "delete", "download", "upload", "delete-asset", "verify-asset"))
	root.AddCommand(group("run", "list", "view", "watch", "download", "rerun", "cancel", "delete"))
	root.AddCommand(group("workflow", "list", "view", "run", "enable", "disable"))
	root.AddCommand(group("search", "code", "commits", "issues", "prs", "repos"))
	root.AddCommand(group("secret", "list", "set", "delete"))
	root.AddCommand(group("variable", "get", "list", "set", "delete"))
	root.AddCommand(group("cache", "list", "delete"))
	root.AddCommand(group("label", "list", "create", "edit", "clone", "delete"))
	root.AddCommand(group("ssh-key", "list", "add", "delete"))
	root.AddCommand(group("gist", "list", "view", "create", "edit", "rename", "clone", "delete"))

	project := group("project", "list", "view", "create", "edit", "copy", "close", "delete", "item-list", "item-add", "item-create", "item-edit", "item-delete", "item-archive", "field-list", "field-create", "field-delete", "mark-template", "link", "unlink")
	root.AddCommand(project)

	root.AddCommand(group("attestation", "verify", "download", "trusted-root"))
	root.AddCommand(group("auth", "login", "logout", "status", "token", "refresh", "setup-git"))

	// Host-coupled namespaces (local side effects), denied by default in HTTP mode.
	root.AddCommand(group("extension", "list", "install", "upgrade", "remove", "create", "exec", "browse", "search"))
	root.AddCommand(group("alias", "list", "set", "delete", "import"))
	root.AddCommand(group("config", "get", "set", "list", "clear-cache"))
	root.AddCommand(group("codespace", "list", "create", "delete", "ssh", "cp", "code", "logs", "ports", "stop", "rebuild"))
	root.AddCommand(group("skill", "list", "search", "install", "preview"))

	root.AddCommand(leaf("api"))
	root.AddCommand(leaf("status"))
	root.AddCommand(leaf("browse"))
	root.AddCommand(leaf("version"))

	return root
}

func TestClassifyArgs(t *testing.T) {
	root := fakeRoot()

	tests := []struct {
		name     string
		args     []string
		wantCat  category
		wantTop  string
		resolved bool
	}{
		// reads
		{"issue list", []string{"issue", "list"}, categoryRead, "issue", true},
		{"issue list with repo flag", []string{"issue", "list", "--repo", "cli/cli"}, categoryRead, "issue", true},
		{"issue list short repo flag", []string{"issue", "list", "-R", "cli/cli", "--web"}, categoryRead, "issue", true},
		{"issue view positional", []string{"issue", "view", "123"}, categoryRead, "issue", true},
		{"issue status", []string{"issue", "status"}, categoryRead, "issue", true},
		{"pr diff", []string{"pr", "diff", "42"}, categoryRead, "pr", true},
		{"pr checks", []string{"pr", "checks"}, categoryRead, "pr", true},
		{"run watch", []string{"run", "watch", "9"}, categoryRead, "run", true},
		{"run download", []string{"run", "download"}, categoryRead, "run", true},
		{"release download", []string{"release", "download", "v1"}, categoryRead, "release", true},
		{"release verify-asset", []string{"release", "verify-asset", "v1", "a"}, categoryRead, "release", true},
		{"search issues", []string{"search", "issues", "needle"}, categoryRead, "search", true},
		{"search code", []string{"search", "code", "needle"}, categoryRead, "search", true},
		{"variable get", []string{"variable", "get", "X"}, categoryRead, "variable", true},
		{"project item-list", []string{"project", "item-list", "1"}, categoryRead, "project", true},
		{"repo credits", []string{"repo", "credits"}, categoryRead, "repo", true},
		{"repo garden", []string{"repo", "garden"}, categoryRead, "repo", true},
		{"deploy-key list", []string{"repo", "deploy-key", "list"}, categoryRead, "repo", true},
		{"attestation verify", []string{"attestation", "verify", "x"}, categoryRead, "attestation", true},
		{"attestation download", []string{"attestation", "download", "x"}, categoryRead, "attestation", true},
		{"status", []string{"status"}, categoryRead, "status", true},
		{"browse", []string{"browse"}, categoryRead, "browse", true},
		{"version", []string{"version"}, categoryRead, "version", true},
		{"bare group prints help", []string{"issue"}, categoryRead, "issue", true},

		// writes
		{"issue create", []string{"issue", "create", "--title", "x"}, categoryWrite, "issue", true},
		{"issue edit", []string{"issue", "edit", "1"}, categoryWrite, "issue", true},
		{"issue comment", []string{"issue", "comment", "1"}, categoryWrite, "issue", true},
		{"issue close", []string{"issue", "close", "1"}, categoryWrite, "issue", true},
		{"issue develop", []string{"issue", "develop", "1"}, categoryWrite, "issue", true},
		{"issue transfer", []string{"issue", "transfer", "1"}, categoryWrite, "issue", true},
		{"issue lock", []string{"issue", "lock", "1"}, categoryWrite, "issue", true},
		{"pr merge", []string{"pr", "merge", "1"}, categoryWrite, "pr", true},
		{"pr review", []string{"pr", "review", "1"}, categoryWrite, "pr", true},
		{"pr ready", []string{"pr", "ready", "1"}, categoryWrite, "pr", true},
		{"pr revert", []string{"pr", "revert", "1"}, categoryWrite, "pr", true},
		{"pr checkout", []string{"pr", "checkout", "1"}, categoryWrite, "pr", true},
		{"pr update-branch", []string{"pr", "update-branch", "1"}, categoryWrite, "pr", true},
		{"repo clone", []string{"repo", "clone", "cli/cli"}, categoryWrite, "repo", true},
		{"repo fork", []string{"repo", "fork"}, categoryWrite, "repo", true},
		{"repo sync", []string{"repo", "sync"}, categoryWrite, "repo", true},
		{"repo rename", []string{"repo", "rename", "x"}, categoryWrite, "repo", true},
		{"repo archive", []string{"repo", "archive"}, categoryWrite, "repo", true},
		{"deploy-key add", []string{"repo", "deploy-key", "add", "k"}, categoryWrite, "repo", true},
		{"release create", []string{"release", "create", "v1"}, categoryWrite, "release", true},
		{"release upload", []string{"release", "upload", "v1", "a"}, categoryWrite, "release", true},
		{"run rerun", []string{"run", "rerun", "9"}, categoryWrite, "run", true},
		{"run cancel", []string{"run", "cancel", "9"}, categoryWrite, "run", true},
		{"workflow run", []string{"workflow", "run", "ci"}, categoryWrite, "workflow", true},
		{"workflow enable", []string{"workflow", "enable", "ci"}, categoryWrite, "workflow", true},
		{"secret set", []string{"secret", "set", "X"}, categoryWrite, "secret", true},
		{"variable set", []string{"variable", "set", "X"}, categoryWrite, "variable", true},
		{"label create", []string{"label", "create", "bug"}, categoryWrite, "label", true},
		{"label clone", []string{"label", "clone", "src"}, categoryWrite, "label", true},
		{"ssh-key add", []string{"ssh-key", "add", "k"}, categoryWrite, "ssh-key", true},
		{"gist rename", []string{"gist", "rename", "a", "b"}, categoryWrite, "gist", true},
		{"project item-archive", []string{"project", "item-archive"}, categoryWrite, "project", true},
		{"project item-create", []string{"project", "item-create"}, categoryWrite, "project", true},
		{"project mark-template", []string{"project", "mark-template", "1"}, categoryWrite, "project", true},
		{"project link", []string{"project", "link", "1"}, categoryWrite, "project", true},

		// deletes
		{"issue delete", []string{"issue", "delete", "1", "--yes"}, categoryDelete, "issue", true},
		{"repo delete", []string{"repo", "delete", "x", "--yes"}, categoryDelete, "repo", true},
		{"deploy-key delete", []string{"repo", "deploy-key", "delete", "1"}, categoryDelete, "repo", true},
		{"release delete", []string{"release", "delete", "v1"}, categoryDelete, "release", true},
		{"release delete-asset", []string{"release", "delete-asset", "v1", "a"}, categoryDelete, "release", true},
		{"run delete", []string{"run", "delete", "9"}, categoryDelete, "run", true},
		{"secret delete", []string{"secret", "delete", "X"}, categoryDelete, "secret", true},
		{"variable delete", []string{"variable", "delete", "X"}, categoryDelete, "variable", true},
		{"cache delete", []string{"cache", "delete", "1"}, categoryDelete, "cache", true},
		{"label delete", []string{"label", "delete", "bug"}, categoryDelete, "label", true},
		{"ssh-key delete", []string{"ssh-key", "delete", "1"}, categoryDelete, "ssh-key", true},
		{"gist delete", []string{"gist", "delete", "1"}, categoryDelete, "gist", true},
		{"project delete", []string{"project", "delete", "1"}, categoryDelete, "project", true},
		{"project item-delete", []string{"project", "item-delete"}, categoryDelete, "project", true},
		{"project field-delete", []string{"project", "field-delete"}, categoryDelete, "project", true},

		// special routing
		{"api is its own thing", []string{"api", "repos/cli/cli"}, categoryWrite, "api", true},
		{"auth login resolves", []string{"auth", "login"}, categoryWrite, "auth", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cat, top, _, resolved := classifyArgs(root, tt.args)
			assert.Equal(t, tt.wantCat, cat, "category for %v", tt.args)
			assert.Equal(t, tt.wantTop, top, "top-level for %v", tt.args)
			assert.Equal(t, tt.resolved, resolved, "resolved for %v", tt.args)
		})
	}
}

func TestClassifyArgsUnresolved(t *testing.T) {
	root := fakeRoot()
	_, top, _, resolved := classifyArgs(root, []string{"frobnicate", "widgets"})
	require.False(t, resolved)
	assert.Equal(t, "frobnicate", top)
}

func TestClassifyLeaf(t *testing.T) {
	tests := []struct {
		leaf string
		want category
	}{
		{"list", categoryRead},
		{"view", categoryRead},
		{"create", categoryWrite},
		{"edit", categoryWrite},
		{"delete", categoryDelete},
		{"item-delete", categoryDelete},
		{"delete-asset", categoryDelete},
		{"remove", categoryDelete},
		{"unknown-verb", categoryWrite}, // safe default
	}
	for _, tt := range tests {
		t.Run(tt.leaf, func(t *testing.T) {
			assert.Equal(t, tt.want, classifyLeaf(tt.leaf))
		})
	}
}
