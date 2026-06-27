package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/spf13/cobra"
)

const (
	mcpPath = "/mcp"
	// prmPath is the well-known location for OAuth Protected Resource Metadata
	// (RFC 9728). The MCP variant appends the resource path.
	prmPath    = "/.well-known/oauth-protected-resource"
	prmPathMCP = "/.well-known/oauth-protected-resource/mcp"
)

// runHTTP serves the MCP server over the streamable HTTP transport, alongside
// OAuth Protected Resource Metadata endpoints (RFC 9728), mirroring the
// behaviour of the official github/github-mcp-server.
//
// The server is built per request from base config layered with policy headers,
// so a single process can serve callers with different allow/deny/read-only
// policies.
func runHTTP(ctx context.Context, cfg serverConfig, root *cobra.Command, addr string, io *iostreams.IOStreams) error {
	handler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return newServer(cfg.withHeaders(req), root)
	}, &mcp.StreamableHTTPOptions{Stateless: true})

	mux := http.NewServeMux()
	mux.Handle(mcpPath, handler)
	mux.Handle(mcpPath+"/", handler)
	mux.HandleFunc(prmPath, protectedResourceMetadataHandler)
	mux.HandleFunc(prmPathMCP, protectedResourceMetadataHandler)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	fmt.Fprintf(io.ErrOut, "gh MCP server listening on http://%s%s (%s)\n", addr, mcpPath, toolSummary(cfg))
	fmt.Fprintf(io.ErrOut, "protected resource metadata at http://%s%s\n", addr, prmPath)

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// protectedResourceMetadataHandler serves OAuth Protected Resource Metadata so
// that MCP hosts can discover the GitHub authorization server to use.
func protectedResourceMetadataHandler(w http.ResponseWriter, r *http.Request) {
	resource := externalURL(r) + mcpPath
	meta := oauthex.ProtectedResourceMetadata{
		Resource:               resource,
		AuthorizationServers:   []string{authorizationServer()},
		BearerMethodsSupported: []string{"header"},
		ScopesSupported:        []string{"repo", "read:org", "gist"},
		ResourceName:           "GitHub CLI MCP server",
		ResourceDocumentation:  "https://github.com/cli/cli",
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(meta)
}

// authorizationServer returns the OAuth authorization server issuer for the
// configured GitHub host (github.com by default, or a GHES host via GH_HOST).
func authorizationServer() string {
	host := strings.TrimSpace(os.Getenv("GH_HOST"))
	if host == "" {
		host = "github.com"
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "https://"), "/")
	return "https://" + host
}

// externalURL reconstructs the scheme://host base URL for the incoming request.
func externalURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = fwd
	}
	return scheme + "://" + host
}
