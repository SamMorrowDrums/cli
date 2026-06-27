package mcp

import (
	"context"
	"fmt"
	"os"

	"github.com/MakeNowJust/heredoc"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

type mcpOptions struct {
	io         *iostreams.IOStreams
	ghPath     string
	appVersion string

	// rootCmd is the live gh command tree, captured in RunE so command
	// classification can reuse gh's own parsing.
	rootCmd *cobra.Command

	mode       string
	readOnly   bool
	allowLocal bool
	httpAddr   string
	allow      []string
	deny       []string
}

// NewCmdMcp creates the `gh mcp` command, which runs a Model Context Protocol
// server backed by the gh CLI.
func NewCmdMcp(f *cmdutil.Factory) *cobra.Command {
	opts := &mcpOptions{
		io:         f.IOStreams,
		ghPath:     f.ExecutablePath,
		appVersion: f.AppVersion,
	}

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Start a Model Context Protocol (MCP) server backed by gh",
		Long: heredoc.Docf(`
			Start a Model Context Protocol (MCP) server that exposes the GitHub CLI
			through a deliberately tiny set of tools.

			Instead of one tool per GitHub operation, this server exposes the gh CLI
			itself, split by intent so that hosts can reason about risk:

			  - %[1]sgh%[1]s         run a read-only command (issue list, pr view, search, ...)
			  - %[1]sgh_write%[1]s   run a state-changing command (issue create, pr merge, ...)
			  - %[1]sgh_delete%[1]s  run a destructive command (issue delete, repo delete, ...)
			  - %[1]sgh_api%[1]s     make a raw authenticated API request (gh api ...)

			Each tool takes the gh argument vector plus two optional, in-process
			post-processors applied in a fixed order: %[1]sgh <args> | jq | grep%[1]s.

			Every command is classified as read, write, or delete and the split is
			enforced: calling the wrong tool for a command is rejected. The gh auth
			commands are never available.

			Configuration (environment variables, or per-request headers in HTTP mode):

			  GH_MCP_READ_ONLY          register only the read-only gh tool
			  GH_MCP_ALLOWED_COMMANDS   comma-separated allowlist of top-level commands
			  GH_MCP_DENIED_COMMANDS    comma-separated denylist of top-level commands

			  X-GH-MCP-Read-Only, X-GH-MCP-Allowed-Commands, X-GH-MCP-Denied-Commands
		`, "`"),
		Example: heredoc.Doc(`
			# Run over stdio (default), for a local MCP host
			$ gh mcp

			# Expose only read-only tools
			$ gh mcp --read-only

			# Restrict to issues and pull requests
			$ gh mcp --allow-commands issue,pr

			# Run as a streamable HTTP server
			$ gh mcp --mode http --http-addr 127.0.0.1:8765
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.rootCmd = cmd.Root()
			return mcpRun(cmd.Context(), opts)
		},
	}

	cmdutil.StringEnumFlag(cmd, &opts.mode, "mode", "", "stdio", []string{"stdio", "http"}, "Transport to serve the MCP server over")
	cmd.Flags().BoolVar(&opts.readOnly, "read-only", false, "Only register the read-only `gh` tool")
	cmd.Flags().BoolVar(&opts.allowLocal, "allow-local", false, "In `--mode http`, permit commands with local-machine side effects (clone, checkout, downloads, extension, config, ...)")
	cmd.Flags().StringVar(&opts.httpAddr, "http-addr", "127.0.0.1:8765", "Address to listen on in `--mode http`")
	cmd.Flags().StringSliceVar(&opts.allow, "allow-commands", nil, "Allowlist of top-level gh commands (overrides GH_MCP_ALLOWED_COMMANDS)")
	cmd.Flags().StringSliceVar(&opts.deny, "deny-commands", nil, "Denylist of top-level gh commands (overrides GH_MCP_DENIED_COMMANDS)")

	// Starting the server should not require authentication; individual gh
	// invocations enforce auth themselves.
	cmdutil.DisableAuthCheck(cmd)

	return cmd
}

func mcpRun(ctx context.Context, opts *mcpOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}

	cfg := configFromEnv(opts.ghPath, opts.appVersion, opts.readOnly)
	if opts.allow != nil {
		cfg.allowed = sliceToSet(opts.allow)
	}
	if opts.deny != nil {
		cfg.denied = sliceToSet(opts.deny)
	}

	switch opts.mode {
	case "http":
		// On a shared HTTP server, deny local-machine commands by default. The
		// --allow-local flag or GH_MCP_ALLOW_LOCAL re-enables them; per-request
		// headers can still override.
		cfg.denyLocal = !(opts.allowLocal || parseBool(os.Getenv(envAllowLocal)))
		return runHTTP(ctx, cfg, opts.rootCmd, opts.httpAddr, opts.io)
	default:
		// Local stdio is a single trusted user: expose the full gh surface.
		cfg.denyLocal = false
		return runStdio(ctx, cfg, opts.rootCmd, opts.io)
	}
}

// runStdio serves the MCP server over stdin/stdout. Status output must go to
// stderr so it does not corrupt the protocol stream on stdout.
func runStdio(ctx context.Context, cfg serverConfig, root *cobra.Command, io *iostreams.IOStreams) error {
	server := newServer(cfg, root)
	fmt.Fprintf(io.ErrOut, "gh MCP server ready on stdio (%s)\n", toolSummary(cfg))
	return server.Run(ctx, &mcp.StdioTransport{})
}

// toolSummary describes which tools are registered, for status output.
func toolSummary(cfg serverConfig) string {
	if cfg.readOnly {
		return "read-only: gh"
	}
	return "tools: gh, gh_write, gh_delete, gh_api"
}

func sliceToSet(s []string) map[string]bool {
	if len(s) == 0 {
		return nil
	}
	set := map[string]bool{}
	for _, v := range s {
		if v != "" {
			set[v] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}
