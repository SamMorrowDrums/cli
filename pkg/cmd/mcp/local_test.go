package mcp

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDenyLocalInHTTPMode(t *testing.T) {
	root := fakeRoot()
	httpCfg := serverConfig{ghPath: "gh", denyLocal: true}
	localCfg := serverConfig{ghPath: "gh", denyLocal: false}

	localCommands := [][]string{
		{"repo", "clone", "cli/cli"},
		{"pr", "checkout", "1"},
		{"run", "download", "9"},
		{"release", "download", "v1"},
		{"repo", "sync"},
		{"gist", "clone", "abc"},
		{"extension", "list"},
		{"config", "get", "editor"},
		{"alias", "list"},
		{"codespace", "list"},
	}

	for _, args := range localCommands {
		// Pick the tool category that matches so we isolate the local-deny path.
		want, _, _, _ := classifyArgs(root, args)
		t.Run("http denies "+args[0]+" "+args[len(args)-1], func(t *testing.T) {
			msg := httpCfg.authorize(root, args, want)
			assert.Contains(t, msg, "local-machine side effects")
		})
		t.Run("stdio allows "+args[0]+" "+args[len(args)-1], func(t *testing.T) {
			assert.Equal(t, "", localCfg.authorize(root, args, want))
		})
	}
}

func TestDenyLocalDoesNotBlockRemote(t *testing.T) {
	root := fakeRoot()
	cfg := serverConfig{ghPath: "gh", denyLocal: true}

	// Remote-only commands are unaffected by denyLocal.
	assert.Equal(t, "", cfg.authorize(root, []string{"issue", "list"}, categoryRead))
	assert.Equal(t, "", cfg.authorize(root, []string{"issue", "create"}, categoryWrite))
	assert.Equal(t, "", cfg.authorize(root, []string{"secret", "set", "X"}, categoryWrite))
}

func TestAllowLocalHeaderOverridesDenyLocal(t *testing.T) {
	base := serverConfig{ghPath: "gh", denyLocal: true}

	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set(headerAllowLocal, "true")
	got := base.withHeaders(req)
	assert.False(t, got.denyLocal)

	req2 := httptest.NewRequest("POST", "/mcp", nil)
	got2 := base.withHeaders(req2)
	assert.True(t, got2.denyLocal, "denyLocal unchanged without header")
}

func TestIsLocalCommand(t *testing.T) {
	assert.True(t, isLocalCommand("extension", "list"))
	assert.True(t, isLocalCommand("repo", "clone"))
	assert.True(t, isLocalCommand("run", "download"))
	assert.False(t, isLocalCommand("issue", "list"))
	assert.False(t, isLocalCommand("secret", "set"))
}
