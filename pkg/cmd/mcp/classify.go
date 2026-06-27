package mcp

import (
	"strings"

	"github.com/spf13/cobra"
)

// category is the read/write/delete classification of a resolved gh command.
// It drives the "hard split" between the gh, gh_write, and gh_delete tools.
type category int

const (
	categoryRead category = iota
	categoryWrite
	categoryDelete
)

func (c category) String() string {
	switch c {
	case categoryRead:
		return "read"
	case categoryWrite:
		return "write"
	case categoryDelete:
		return "delete"
	default:
		return "unknown"
	}
}

// readLeaves are leaf subcommand names that never modify state (local or remote).
// The classifier defaults unknown leaves to write, so this set only needs the
// genuinely read-only verbs. It must never contain a verb that mutates anything.
var readLeaves = map[string]bool{
	"list":         true,
	"view":         true,
	"get":          true,
	"status":       true,
	"checks":       true,
	"check":        true,
	"diff":         true,
	"download":     true,
	"watch":        true,
	"verify":       true,
	"verify-asset": true,
	"read-file":    true,
	"read-dir":     true,
	"credits":      true,
	"gitignore":    true,
	"license":      true,
	"code":         true, // search code
	"commits":      true, // search commits
	"issues":       true, // search issues
	"prs":          true, // search prs
	"repos":        true, // search repos
	"item-list":    true, // project item-list
	"field-list":   true, // project field-list
	"logs":         true, // codespace logs
	"ports":        true, // codespace ports (forward is a child)
	"trusted-root": true, // attestation trusted-root
	"browse":       true, // opens a browser / TUI, no data mutation
	"garden":       true, // interactive visualization, no mutation
	"completion":   true,
	"version":      true,
}

// readonlyNamespaces are top-level commands whose entire subtree is read-only.
var readonlyNamespaces = map[string]bool{
	"search":      true,
	"attestation": true,
}

// pathOverrides pins specific full command paths (e.g. "repo sync") to a
// category when the leaf-verb heuristic would be wrong. Keep this small; prefer
// fixing the heuristic when a pattern generalizes.
var pathOverrides = map[string]category{}

// classifyArgs resolves args against the live gh command tree and classifies the
// target command. topLevel is the first command segment after "gh" (used for
// auth blocking and allow/deny lists), leaf is the resolved leaf command name
// (used for local-side-effect detection). resolved reports whether args mapped
// onto a real command in the tree.
func classifyArgs(root *cobra.Command, args []string) (cat category, topLevel, leaf string, resolved bool) {
	target, _, err := root.Find(args)
	if err != nil || target == nil {
		// Fall back to a best-effort guess from the raw args so callers can
		// still produce a useful error message.
		return classifyRawArgs(args), firstCommandToken(args), lastCommandToken(args), false
	}

	if target == root {
		return categoryRead, firstCommandToken(args), lastCommandToken(args), false
	}

	// Walk up to the direct child of root to find the top-level command name.
	top := target
	for top.Parent() != nil && top.Parent() != root {
		top = top.Parent()
	}
	topLevel = top.Name()
	leaf = target.Name()

	path := strings.TrimPrefix(target.CommandPath(), root.Name()+" ")
	return classifyResolved(path, leaf, topLevel, target.Runnable()), topLevel, leaf, true
}

// classifyResolved applies the override map, read-only namespaces, group rule,
// and leaf-verb heuristic in priority order.
func classifyResolved(path, leaf, topLevel string, runnable bool) category {
	if c, ok := pathOverrides[path]; ok {
		return c
	}
	if readonlyNamespaces[topLevel] {
		return categoryRead
	}
	// Pure command groups (no Run) only ever print help.
	if !runnable {
		return categoryRead
	}
	return classifyLeaf(leaf)
}

// classifyLeaf maps a leaf subcommand name to a category. Anything containing
// "delete" or "remove" is a delete; the curated read set is read; everything
// else defaults to write. Defaulting to write is the safe choice: a misclassified
// read is merely inconvenient (use gh_write), whereas a misclassified write must
// never be reachable from the read-only tool.
func classifyLeaf(leaf string) category {
	if strings.Contains(leaf, "delete") || strings.Contains(leaf, "remove") {
		return categoryDelete
	}
	if readLeaves[leaf] {
		return categoryRead
	}
	return categoryWrite
}

// classifyRawArgs is a heuristic used only when the command cannot be resolved
// against the tree (e.g. a typo). It picks the last non-flag token as the leaf.
func classifyRawArgs(args []string) category {
	leaf := ""
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		leaf = a
	}
	if leaf == "" {
		return categoryRead
	}
	return classifyLeaf(leaf)
}

// firstCommandToken returns the first non-flag token in args, or "".
func firstCommandToken(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

// lastCommandToken returns the last non-flag token in args, or "".
func lastCommandToken(args []string) string {
	leaf := ""
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			leaf = a
		}
	}
	return leaf
}

// localNamespaces are top-level commands tied to the server's local machine,
// installed software, or host gh configuration rather than to GitHub data.
// They are denied by default in HTTP mode, where the filesystem and gh config
// are shared across callers.
var localNamespaces = map[string]bool{
	"extension": true, // installs and executes local binaries
	"alias":     true, // edits host gh config; shell aliases execute code
	"config":    true, // mutates host gh config for every caller
	"skill":     true, // installs/executes skills on the host
	"codespace": true, // ssh/cp/port-forward against the host
}

// localLeaves are leaf verbs that write to the local filesystem even under an
// otherwise remote namespace (e.g. repo clone, run download). Denied by default
// in HTTP mode.
var localLeaves = map[string]bool{
	"clone":    true, // repo clone, gist clone
	"checkout": true, // pr checkout
	"download": true, // run download, release download
	"sync":     true, // repo sync (touches a local clone)
}

// isLocalCommand reports whether a command has local-machine side effects that
// make it unsafe to expose on a shared (HTTP) server.
func isLocalCommand(topLevel, leaf string) bool {
	if localNamespaces[topLevel] {
		return true
	}
	return localLeaves[leaf]
}
