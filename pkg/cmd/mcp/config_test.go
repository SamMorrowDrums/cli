package mcp

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCommandSet(t *testing.T) {
	assert.Nil(t, parseCommandSet(""))
	assert.Nil(t, parseCommandSet("  "))
	assert.Equal(t, map[string]bool{"issue": true, "pr": true}, parseCommandSet("issue, pr"))
	assert.Equal(t, map[string]bool{"repo": true}, parseCommandSet(" repo ,, "))
}

func TestParseBool(t *testing.T) {
	for _, s := range []string{"1", "true", "TRUE", "Yes", "on"} {
		assert.True(t, parseBool(s), s)
	}
	for _, s := range []string{"", "0", "false", "no", "off", "nope"} {
		assert.False(t, parseBool(s), s)
	}
}

func TestCommandAllowed(t *testing.T) {
	t.Run("hard-denied auth", func(t *testing.T) {
		cfg := serverConfig{}
		ok, reason := cfg.commandAllowed("auth")
		assert.False(t, ok)
		assert.Contains(t, reason, "not available")
	})
	t.Run("hard-denied mcp", func(t *testing.T) {
		cfg := serverConfig{}
		ok, _ := cfg.commandAllowed("mcp")
		assert.False(t, ok)
	})
	t.Run("denylist", func(t *testing.T) {
		cfg := serverConfig{denied: map[string]bool{"repo": true}}
		ok, reason := cfg.commandAllowed("repo")
		assert.False(t, ok)
		assert.Contains(t, reason, "disabled by server configuration")
	})
	t.Run("allowlist excludes others", func(t *testing.T) {
		cfg := serverConfig{allowed: map[string]bool{"issue": true}}
		ok, _ := cfg.commandAllowed("pr")
		assert.False(t, ok)
		ok, _ = cfg.commandAllowed("issue")
		assert.True(t, ok)
	})
	t.Run("nil allowlist allows all", func(t *testing.T) {
		cfg := serverConfig{}
		ok, _ := cfg.commandAllowed("anything")
		assert.True(t, ok)
	})
	t.Run("empty top-level allowed", func(t *testing.T) {
		cfg := serverConfig{}
		ok, _ := cfg.commandAllowed("")
		assert.True(t, ok)
	})
}

func TestWithHeaders(t *testing.T) {
	base := serverConfig{ghPath: "gh"}

	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set(headerReadOnly, "1")
	req.Header.Set(headerAllowed, "issue,pr")
	req.Header.Set(headerDenied, "repo")

	got := base.withHeaders(req)
	assert.True(t, got.readOnly)
	assert.Equal(t, map[string]bool{"issue": true, "pr": true}, got.allowed)
	assert.Equal(t, map[string]bool{"repo": true}, got.denied)

	// base is unchanged
	assert.False(t, base.readOnly)
	require.Nil(t, base.allowed)
}
