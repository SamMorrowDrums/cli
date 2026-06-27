package mcp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/cli/go-gh/v2/pkg/jq"
)

// runResult is the outcome of a gh invocation plus optional jq/grep processing.
type runResult struct {
	// output is the text returned to the model (processed stdout, or stderr on
	// failure).
	output string
	// isError reports whether the invocation should be surfaced as a tool error.
	isError bool
}

// runGH executes `gh <args>` as a subprocess, then applies the optional jq and
// grep post-processing in a fixed order: stdout -> jq -> grep. This mirrors the
// shell pipeline `gh <args> | jq <jqExpr> | grep <grepExpr>`.
//
// Security: this function never invokes a shell, which is what makes it safe to
// expose over HTTP.
//   - gh is run via exec with an explicit argv (no "sh -c"), so argument values
//     cannot be interpreted as shell metacharacters / command separators.
//   - jq is evaluated in-process by gojq (go-gh's jq package), not the jq binary.
//   - grep is implemented with Go's RE2 regexp engine (linear time, no ReDoS),
//     not the grep binary.
//
// The remaining code-execution surface belongs to gh itself (shell aliases and
// extensions); operators can lock that down with the allow/deny lists and
// read-only mode.
func runGH(ctx context.Context, ghPath string, args []string, jqExpr, grepExpr string) runResult {
	if ghPath == "" {
		ghPath = "gh"
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, ghPath, args...)
	cmd.Stdin = bytes.NewReader(nil)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = ghEnv()

	runErr := cmd.Run()

	if runErr != nil {
		// Command failed: return stderr (and any stdout) as an error so the model
		// can self-correct. jq/grep are intentionally skipped on failure.
		msg := strings.TrimRight(stderr.String(), "\n")
		if out := strings.TrimRight(stdout.String(), "\n"); out != "" {
			if msg != "" {
				msg += "\n"
			}
			msg += out
		}
		if msg == "" {
			msg = runErr.Error()
		}
		return runResult{output: msg, isError: true}
	}

	data := stdout.Bytes()

	processed, err := postProcess(data, jqExpr, grepExpr)
	if err != nil {
		return runResult{output: err.Error(), isError: true}
	}
	data = processed

	out := strings.TrimRight(string(data), "\n")
	if out == "" {
		// Some gh commands print human-readable confirmations to stderr while
		// producing no stdout (e.g. "✓ Created issue ..."). Surface that.
		if s := strings.TrimRight(stderr.String(), "\n"); s != "" {
			out = s
		}
	}
	return runResult{output: out, isError: false}
}

// postProcess applies the optional jq then grep stages to data, in that fixed
// order. Both stages run in-process (gojq and Go's RE2 regexp), so no shell or
// external binary is ever spawned.
func postProcess(data []byte, jqExpr, grepExpr string) ([]byte, error) {
	if jqExpr != "" {
		var buf bytes.Buffer
		if err := jq.Evaluate(bytes.NewReader(data), &buf, jqExpr); err != nil {
			return nil, fmt.Errorf("jq error: %v\n(jq is applied to command stdout, which must be valid JSON)", err)
		}
		data = buf.Bytes()
	}

	if grepExpr != "" {
		filtered, err := grepLines(data, grepExpr)
		if err != nil {
			return nil, fmt.Errorf("grep error: %v", err)
		}
		data = filtered
	}

	return data, nil
}

// grepLines keeps only the lines of data that match the given regular
// expression, preserving order.
func grepLines(data []byte, pattern string) ([]byte, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	for _, line := range strings.Split(string(data), "\n") {
		if re.MatchString(line) {
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes(), nil
}

// ghEnv returns the environment for the gh subprocess: the current environment
// plus a couple of tweaks to keep output clean and non-interactive-friendly.
func ghEnv() []string {
	env := os.Environ()
	env = append(env,
		"GH_NO_UPDATE_NOTIFIER=1",
		"GH_PROMPT_DISABLED=1",
	)
	return env
}
