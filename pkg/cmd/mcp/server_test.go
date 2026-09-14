package mcp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAuthorizeHardSplit(t *testing.T) {
	root := fakeRoot()
	cfg := serverConfig{ghPath: "gh"}

	tests := []struct {
		name      string
		want      category
		args      []string
		wantOK    bool   // "" message
		wantPhras string // substring expected in rejection message
	}{
		{"read tool runs read", categoryRead, []string{"issue", "list"}, true, ""},
		{"read tool rejects write", categoryRead, []string{"issue", "create"}, false, "gh_write"},
		{"read tool rejects delete", categoryRead, []string{"issue", "delete", "1"}, false, "gh_delete"},
		{"write tool runs write", categoryWrite, []string{"issue", "create"}, true, ""},
		{"write tool rejects read", categoryWrite, []string{"issue", "list"}, false, "gh tool"},
		{"write tool rejects delete", categoryWrite, []string{"issue", "delete", "1"}, false, "gh_delete"},
		{"delete tool runs delete", categoryDelete, []string{"issue", "delete", "1"}, true, ""},
		{"delete tool rejects read", categoryDelete, []string{"issue", "list"}, false, "gh tool"},
		{"delete tool rejects write", categoryDelete, []string{"issue", "create"}, false, "gh_write"},
		{"api routed to gh_api from read", categoryRead, []string{"api", "repos/cli/cli"}, false, "gh_api"},
		{"api routed to gh_api from write", categoryWrite, []string{"api", "repos/cli/cli"}, false, "gh_api"},
		{"auth blocked from read", categoryRead, []string{"auth", "login"}, false, "not available"},
		{"auth blocked from write", categoryWrite, []string{"auth", "login"}, false, "not available"},
		{"mcp recursion blocked", categoryRead, []string{"mcp"}, false, "not available"},
		{"empty args rejected", categoryRead, nil, false, "no args"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := cfg.authorize(root, tt.args, tt.want)
			if tt.wantOK {
				assert.Equal(t, "", msg, "expected command to be authorized")
				return
			}
			assert.NotEqual(t, "", msg, "expected a rejection message")
			if tt.wantPhras != "" {
				assert.Contains(t, msg, tt.wantPhras)
			}
		})
	}
}

func TestAuthorizeAllowList(t *testing.T) {
	root := fakeRoot()
	cfg := serverConfig{ghPath: "gh", allowed: map[string]bool{"issue": true, "pr": true}}

	// allowed command passes
	assert.Equal(t, "", cfg.authorize(root, []string{"issue", "list"}, categoryRead))
	// not-allowed command is rejected
	msg := cfg.authorize(root, []string{"repo", "list"}, categoryRead)
	assert.Contains(t, msg, "allowed command list")
}

func TestAuthorizeDenyList(t *testing.T) {
	root := fakeRoot()
	cfg := serverConfig{ghPath: "gh", denied: map[string]bool{"repo": true}}

	assert.Equal(t, "", cfg.authorize(root, []string{"issue", "list"}, categoryRead))
	msg := cfg.authorize(root, []string{"repo", "list"}, categoryRead)
	assert.Contains(t, msg, "disabled by server configuration")
}

func TestAuthorizeAuthAlwaysBlocked(t *testing.T) {
	root := fakeRoot()
	// Even if an operator mistakenly allow-lists auth, it stays hard-blocked.
	cfg := serverConfig{ghPath: "gh", allowed: map[string]bool{"auth": true}}
	msg := cfg.authorize(root, []string{"auth", "login"}, categoryWrite)
	assert.Contains(t, msg, "not available")
}

func TestWrongToolMessageNamesCorrectTool(t *testing.T) {
	assert.Contains(t, wrongToolMessage(categoryRead, categoryWrite, "issue"), toolGhWrite)
	assert.Contains(t, wrongToolMessage(categoryRead, categoryDelete, "issue"), toolGhDelete)
	assert.Contains(t, wrongToolMessage(categoryWrite, categoryRead, "issue"), toolGh)
	// it should also name the offending command and its category
	msg := wrongToolMessage(categoryRead, categoryDelete, "repo")
	assert.Contains(t, msg, "repo")
	assert.True(t, strings.Contains(msg, "delete"))
}
