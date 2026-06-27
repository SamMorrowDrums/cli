package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostProcessJqThenGrep(t *testing.T) {
	input := []byte(`[{"number":1,"title":"alpha bug"},{"number":2,"title":"beta feature"},{"number":3,"title":"gamma bug"}]`)

	t.Run("jq only", func(t *testing.T) {
		out, err := postProcess(input, `.[].title`, "")
		require.NoError(t, err)
		// go-gh's jq.Evaluate renders string scalars raw (unquoted).
		assert.Equal(t, "alpha bug\nbeta feature\ngamma bug\n", string(out))
	})

	t.Run("jq then grep (order matters)", func(t *testing.T) {
		// jq extracts titles, then grep keeps only the "bug" lines.
		out, err := postProcess(input, `.[].title`, "bug")
		require.NoError(t, err)
		assert.Equal(t, "alpha bug\ngamma bug\n", string(out))
	})

	t.Run("grep only", func(t *testing.T) {
		lines := []byte("alpha\nbeta\ngamma\n")
		out, err := postProcess(lines, "", "ph|mm")
		require.NoError(t, err)
		assert.Equal(t, "alpha\ngamma\n", string(out))
	})

	t.Run("no processing returns input", func(t *testing.T) {
		out, err := postProcess([]byte("hello"), "", "")
		require.NoError(t, err)
		assert.Equal(t, "hello", string(out))
	})
}

func TestPostProcessErrors(t *testing.T) {
	t.Run("invalid json for jq", func(t *testing.T) {
		_, err := postProcess([]byte("not json"), ".foo", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "jq error")
	})

	t.Run("invalid regexp for grep", func(t *testing.T) {
		_, err := postProcess([]byte("data"), "", "[")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "grep error")
	})
}

func TestGrepLines(t *testing.T) {
	data := []byte("apple\nbanana\ncherry\napricot\n")
	out, err := grepLines(data, "^ap")
	require.NoError(t, err)
	assert.Equal(t, "apple\napricot\n", string(out))
}
