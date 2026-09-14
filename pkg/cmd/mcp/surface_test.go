package mcp_test

import (
	"testing"

	"github.com/cli/cli/v2/internal/browser"
	"github.com/cli/cli/v2/internal/config"
	"github.com/cli/cli/v2/internal/gh"
	"github.com/cli/cli/v2/internal/telemetry"
	"github.com/cli/cli/v2/pkg/cmd/root"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/extensions"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// TestRealSurfaceAssumptions guards against the real gh command tree drifting
// away from the command names the MCP classifier hardcodes (read-only
// namespaces, local namespaces, and representative leaves). If gh renames or
// removes one of these, this test fails so the maintainer revisits classify.go.
//
// It lives in an external test package because pkg/cmd/root imports pkg/cmd/mcp;
// building the real root from inside package mcp would be an import cycle.
func TestRealSurfaceAssumptions(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	f := &cmdutil.Factory{
		IOStreams: ios,
		Config:    func() (gh.Config, error) { return config.NewBlankConfig(), nil },
		Browser:   &browser.Stub{},
		ExtensionManager: &extensions.ExtensionManagerMock{
			ListFunc: func() []extensions.Extension { return nil },
		},
	}

	rootCmd, err := root.NewCmdRoot(f, &telemetry.NoOpService{}, "1.0.0", "")
	require.NoError(t, err)

	// Top-level commands the classifier reasons about.
	topLevel := []string{
		"issue", "pr", "repo", "release", "run", "workflow", "search",
		"secret", "variable", "cache", "label", "project", "attestation",
		"auth", "api", "status", "extension", "alias", "config", "codespace",
		"skill", "gist", "ssh-key", "gpg-key", "org", "ruleset", "mcp",
	}
	for _, name := range topLevel {
		require.NotNilf(t, findChild(rootCmd, name), "expected top-level command %q to exist", name)
	}

	// Representative leaves whose verbs drive read/write/delete and local
	// classification. {parent path} -> {expected child leaves}.
	leaves := map[string][]string{
		"issue":   {"list", "view", "create", "edit", "delete", "comment", "develop"},
		"pr":      {"list", "view", "diff", "checks", "merge", "checkout", "review", "ready"},
		"repo":    {"view", "clone", "fork", "sync", "rename", "archive", "delete"},
		"release": {"list", "view", "download", "upload", "delete", "delete-asset"},
		"run":     {"list", "view", "watch", "download", "rerun", "cancel", "delete"},
		"search":  {"code", "commits", "issues", "prs", "repos"},
		"secret":  {"list", "set", "delete"},
	}
	for parent, kids := range leaves {
		p := findChild(rootCmd, parent)
		require.NotNilf(t, p, "expected %q to exist", parent)
		for _, kid := range kids {
			require.NotNilf(t, findChild(p, kid), "expected `gh %s %s` to exist", parent, kid)
		}
	}
}

// findChild returns the immediate subcommand named name, or nil.
func findChild(cmd *cobra.Command, name string) *cobra.Command {
	for _, c := range cmd.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}
