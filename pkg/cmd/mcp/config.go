package mcp

import (
	"net/http"
	"os"
	"strings"
)

// Environment variables that configure the server in stdio mode (and provide
// defaults in HTTP mode, which can be overridden per-request by headers).
const (
	envReadOnly = "GH_MCP_READ_ONLY"
	envAllowed  = "GH_MCP_ALLOWED_COMMANDS"
	envDenied   = "GH_MCP_DENIED_COMMANDS"
	// envAllowLocal disables the HTTP-mode denial of local-machine commands.
	envAllowLocal = "GH_MCP_ALLOW_LOCAL"
)

// HTTP headers that configure the server per-request in HTTP mode.
const (
	headerReadOnly = "X-GH-MCP-Read-Only"
	headerAllowed  = "X-GH-MCP-Allowed-Commands"
	headerDenied   = "X-GH-MCP-Denied-Commands"
	// headerAllowLocal disables the HTTP-mode denial of local-machine commands.
	headerAllowLocal = "X-GH-MCP-Allow-Local"
)

// hardDenied top-level commands can never be invoked through the MCP server,
// regardless of allow/deny configuration. `auth` is blocked so the server can
// never manage credentials; `mcp` is blocked to prevent server recursion.
var hardDenied = map[string]bool{
	"auth": true,
	"mcp":  true,
}

// serverConfig is the resolved per-server policy. The same struct is built from
// the environment (stdio) or from request headers layered over the environment
// (HTTP).
type serverConfig struct {
	ghPath     string
	appVersion string

	// readOnly registers only the read-only `gh` tool.
	readOnly bool

	// allowed, when non-nil, is an allowlist of top-level commands. nil means
	// "all commands allowed" (subject to denied and hardDenied).
	allowed map[string]bool
	// denied is a denylist of top-level commands.
	denied map[string]bool

	// denyLocal blocks commands with local-machine side effects (see
	// isLocalCommand). It defaults to true in HTTP mode, where the filesystem
	// and gh config are shared, and false for local stdio use.
	denyLocal bool
}

// configFromEnv builds the base configuration from environment variables.
func configFromEnv(ghPath, appVersion string, readOnlyFlag bool) serverConfig {
	cfg := serverConfig{
		ghPath:     ghPath,
		appVersion: appVersion,
		readOnly:   readOnlyFlag || parseBool(os.Getenv(envReadOnly)),
		allowed:    parseCommandSet(os.Getenv(envAllowed)),
		denied:     parseCommandSet(os.Getenv(envDenied)),
	}
	return cfg
}

// withHeaders returns a copy of cfg with any policy headers from req applied on
// top. Headers override the environment-derived defaults when present.
func (c serverConfig) withHeaders(req *http.Request) serverConfig {
	out := c
	if v := req.Header.Get(headerReadOnly); v != "" {
		out.readOnly = parseBool(v)
	}
	if v := req.Header.Get(headerAllowed); v != "" {
		out.allowed = parseCommandSet(v)
	}
	if v := req.Header.Get(headerDenied); v != "" {
		out.denied = parseCommandSet(v)
	}
	if v := req.Header.Get(headerAllowLocal); v != "" {
		out.denyLocal = !parseBool(v)
	}
	return out
}

// commandAllowed reports whether a top-level command may be run under this
// policy, returning a human-readable reason when it may not.
func (c serverConfig) commandAllowed(topLevel string) (bool, string) {
	if topLevel == "" {
		return true, ""
	}
	if hardDenied[topLevel] {
		return false, "the `" + topLevel + "` command is not available through the gh MCP server"
	}
	if c.denied[topLevel] {
		return false, "the `" + topLevel + "` command is disabled by server configuration"
	}
	if c.allowed != nil && !c.allowed[topLevel] {
		return false, "the `" + topLevel + "` command is not in this server's allowed command list"
	}
	return true, ""
}

// parseCommandSet parses a comma-separated list of top-level command names into
// a set, returning nil for an empty/blank input.
func parseCommandSet(s string) map[string]bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	set := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		if name := strings.TrimSpace(part); name != "" {
			set[name] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// parseBool interprets common truthy strings ("1", "true", "yes", "on").
func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
