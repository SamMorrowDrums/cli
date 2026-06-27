package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

// ghInput is the shared input schema for every tool. Keeping the surface tiny
// (args + two optional post-processors) is the whole point of this experiment:
// lean on the gh CLI that models already know, rather than a wide bespoke tool
// surface.
type ghInput struct {
	// Args is the gh argument vector, parsed exactly as the gh CLI would parse
	// it (no shell involved). Example: ["issue", "list", "--repo", "cli/cli"].
	Args []string `json:"args" jsonschema:"The gh CLI argument vector, e.g. [\"issue\",\"list\",\"--repo\",\"cli/cli\",\"--json\",\"number,title\"]. Do not include the leading \"gh\". Parsed exactly like the gh CLI."`
	// Jq is applied first, to the command's stdout.
	Jq string `json:"jq,omitempty" jsonschema:"Optional jq expression. APPLIED FIRST, to the command's stdout (which must be JSON, e.g. from --json or gh_api). Equivalent to: gh <args> | jq '<jq>'."`
	// Grep is applied second, to the (possibly jq-processed) output.
	Grep string `json:"grep,omitempty" jsonschema:"Optional regular expression. APPLIED SECOND, after jq, keeping only matching output lines. Equivalent to: gh <args> | jq '<jq>' | grep '<grep>'. Pipeline order is always args -> jq -> grep."`
}

const (
	toolGh       = "gh"
	toolGhWrite  = "gh_write"
	toolGhDelete = "gh_delete"
	toolGhAPI    = "gh_api"
)

// newServer constructs an MCP server whose registered tools enforce cfg's
// policy. In read-only mode only the read tool is registered.
func newServer(cfg serverConfig, root *cobra.Command) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "gh",
		Title:   "GitHub CLI",
		Version: versionOrDefault(cfg.appVersion),
	}, nil)

	registerTools(server, cfg, root)
	return server
}

// registerTools adds the read tool always, and the write/delete/api tools only
// when not in read-only mode.
func registerTools(server *mcp.Server, cfg serverConfig, root *cobra.Command) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        toolGh,
		Title:       "Run a read-only gh command",
		Description: readDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:         "Run a read-only gh command",
			ReadOnlyHint:  true,
			OpenWorldHint: boolPtr(true),
		},
	}, categoryHandler(cfg, root, categoryRead))

	if cfg.readOnly {
		return
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        toolGhWrite,
		Title:       "Run a state-changing (non-deleting) gh command",
		Description: writeDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:           "Run a state-changing gh command",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  false,
			OpenWorldHint:   boolPtr(true),
		},
	}, categoryHandler(cfg, root, categoryWrite))

	mcp.AddTool(server, &mcp.Tool{
		Name:        toolGhDelete,
		Title:       "Run a destructive (deleting) gh command",
		Description: deleteDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:           "Run a destructive gh command",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
			IdempotentHint:  false,
			OpenWorldHint:   boolPtr(true),
		},
	}, categoryHandler(cfg, root, categoryDelete))

	mcp.AddTool(server, &mcp.Tool{
		Name:        toolGhAPI,
		Title:       "Make a raw authenticated GitHub API request (gh api)",
		Description: apiDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:           "Raw gh api request",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
			IdempotentHint:  false,
			OpenWorldHint:   boolPtr(true),
		},
	}, apiHandler(cfg, root))
}

// categoryHandler returns a tool handler that enforces the hard read/write/delete
// split: it resolves and classifies the requested command and rejects anything
// that does not match want, blocked top-level commands, and `gh api` (which must
// go through gh_api).
func categoryHandler(cfg serverConfig, root *cobra.Command, want category) mcp.ToolHandlerFor[ghInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ghInput) (*mcp.CallToolResult, any, error) {
		if msg := cfg.authorize(root, in.Args, want); msg != "" {
			return errorResult(msg), nil, nil
		}
		res := runGH(ctx, cfg.ghPath, in.Args, in.Jq, in.Grep)
		return toResult(res), nil, nil
	}
}

// authorize validates a category-tool request and returns a user-facing error
// message, or "" if the request may run. It enforces, in order: non-empty args,
// allow/deny/hard-deny policy, the gh_api routing rule, command resolvability,
// and the hard category split.
func (cfg serverConfig) authorize(root *cobra.Command, args []string, want category) string {
	if len(args) == 0 {
		return "no args provided; pass the gh argument vector, e.g. [\"issue\",\"list\"]"
	}

	cat, topLevel, leaf, resolved := classifyArgs(root, args)

	if ok, reason := cfg.commandAllowed(topLevel); !ok {
		return reason
	}

	if topLevel == "api" {
		return "use the gh_api tool for raw API requests"
	}

	if cfg.denyLocal && isLocalCommand(topLevel, leaf) {
		return fmt.Sprintf(
			"`gh %s` has local-machine side effects and is disabled on this shared (HTTP) server. "+
				"Run `gh mcp` locally over stdio, or set GH_MCP_ALLOW_LOCAL=1 (header X-GH-MCP-Allow-Local: true) to permit it.",
			strings.TrimSpace(topLevel+" "+localLabel(topLevel, leaf)),
		)
	}

	if !resolved {
		return fmt.Sprintf("could not resolve a gh command from args %q", args)
	}

	if cat != want {
		return wrongToolMessage(want, cat, topLevel)
	}

	return ""
}

// localLabel returns the leaf verb to include in a local-denial message when the
// leaf (not the whole namespace) is what makes the command local.
func localLabel(topLevel, leaf string) string {
	if localNamespaces[topLevel] {
		return ""
	}
	return leaf
}

// apiHandler returns the handler for the gh_api escape hatch. It prepends the
// `api` subcommand and runs the request without category enforcement (its
// annotations advertise the risk).
func apiHandler(cfg serverConfig, _ *cobra.Command) mcp.ToolHandlerFor[ghInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ghInput) (*mcp.CallToolResult, any, error) {
		if len(in.Args) == 0 {
			return errorResult("no args provided; pass the gh api argument vector, e.g. [\"repos/cli/cli\"]"), nil, nil
		}
		if ok, reason := cfg.commandAllowed("api"); !ok {
			return errorResult(reason), nil, nil
		}
		args := append([]string{"api"}, in.Args...)
		res := runGH(ctx, cfg.ghPath, args, in.Jq, in.Grep)
		return toResult(res), nil, nil
	}
}

// wrongToolMessage explains which tool should have been used for a mismatched
// category.
func wrongToolMessage(want, got category, topLevel string) string {
	tool := map[category]string{
		categoryRead:   toolGh,
		categoryWrite:  toolGhWrite,
		categoryDelete: toolGhDelete,
	}[got]
	return fmt.Sprintf(
		"`gh %s ...` is a %s command but the %s tool only runs %s commands. Use the %s tool instead.",
		topLevel, got, currentToolName(want), want, tool,
	)
}

func currentToolName(want category) string {
	switch want {
	case categoryRead:
		return toolGh
	case categoryWrite:
		return toolGhWrite
	case categoryDelete:
		return toolGhDelete
	default:
		return toolGh
	}
}

// toResult converts a runResult into an MCP tool result.
func toResult(r runResult) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: r.output}},
		IsError: r.isError,
	}
}

// errorResult builds a tool-error result with the given message.
func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}

func boolPtr(b bool) *bool { return &b }

func versionOrDefault(v string) string {
	if v == "" {
		return "dev"
	}
	return v
}

var readDescription = heredoc.Doc(`
	Run a READ-ONLY gh CLI command and return its output.

	Use this for any command that does not modify state: list, view, get,
	status, checks, diff, search, etc. (e.g. gh issue list, gh pr view,
	gh repo view, gh run view, gh search issues).

	Input:
	  - args: the gh argument vector (no leading "gh"). Prefer adding
	    "--json <fields>" where supported for structured output.
	  - jq:   optional jq expression applied FIRST to stdout (JSON).
	  - grep: optional regex applied SECOND, keeping matching lines.
	  Pipeline order is always: gh <args> | jq | grep.

	Write or delete commands are rejected here; use gh_write or gh_delete.
	Raw API requests must use gh_api. The gh auth commands are not available.
`)

var writeDescription = heredoc.Doc(`
	Run a STATE-CHANGING gh CLI command that creates or updates (but does not
	delete) GitHub resources.

	Examples: gh issue create, gh issue edit, gh issue comment, gh issue close,
	gh pr create, gh pr merge, gh pr review, gh release create, gh workflow run,
	gh repo fork, gh label create.

	Input:
	  - args: the gh argument vector (no leading "gh").
	  - jq:   optional jq expression applied FIRST to stdout (JSON).
	  - grep: optional regex applied SECOND, keeping matching lines.
	  Pipeline order is always: gh <args> | jq | grep.

	Read-only commands are rejected here (use gh); deleting commands are
	rejected here (use gh_delete). Raw API requests must use gh_api.
`)

var deleteDescription = heredoc.Doc(`
	Run a DESTRUCTIVE gh CLI command that deletes or removes a GitHub resource.

	Examples: gh issue delete, gh repo delete, gh release delete,
	gh run delete, gh label delete, gh secret delete, gh cache delete,
	gh ssh-key delete.

	Input:
	  - args: the gh argument vector (no leading "gh"). Many delete commands
	    require "--yes" to skip the interactive confirmation prompt.
	  - jq:   optional jq expression applied FIRST to stdout (JSON).
	  - grep: optional regex applied SECOND, keeping matching lines.
	  Pipeline order is always: gh <args> | jq | grep.

	Only deleting commands are accepted here. Use gh for reads and gh_write
	for non-deleting changes.
`)

var apiDescription = heredoc.Doc(`
	Make a raw authenticated GitHub API request via "gh api". This is the
	escape hatch for anything not covered by the gh subcommands, and it can
	read, write, OR delete depending on the request, so its annotations are set
	to the most cautious (destructive, non-read-only) values.

	Do NOT include the leading "api" in args; it is added for you.

	Input:
	  - args: the gh api argument vector, e.g.
	      ["repos/cli/cli/issues", "--method", "POST", "-f", "title=Bug"]
	      ["graphql", "-f", "query=..."]
	  - jq:   optional jq expression applied FIRST to the JSON response.
	  - grep: optional regex applied SECOND, keeping matching lines.
	  Pipeline order is always: gh api <args> | jq | grep.
`)
