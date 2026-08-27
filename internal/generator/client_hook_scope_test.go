package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratedClientHooksSupportSharedScope(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "client-hook-scope-pp-cli")
	require.NoError(t, New(minimalSpec("client-hook-scope"), outputDir).Generate())

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "^TestSharedClientHookRunsOnBothSurfaces$", "-count=1")
}

func TestGeneratedMCPClientPropagatesHookFailure(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "client-hook-scope-pp-cli")
	require.NoError(t, New(minimalSpec("client-hook-scope"), outputDir).Generate())

	hookFixture := `package cli

import (
	"errors"

	"client-hook-scope-pp-cli/internal/client"
)

func init() {
	registerClientHookFor(clientHookSurfaceMCP, func(*client.Client) error {
		return errors.New("MCP hook failed")
	})
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "cli", "client_hook_fixture.go"),
		[]byte(hookFixture),
		0o644,
	))

	mcpTest := `package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"client-hook-scope-pp-cli/internal/config"
)

func TestMCPClientHookFailurePropagates(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer server.Close()

	ctx := context.Background()
	c, session, err := newMCPClientFromConfig(ctx, &config.Config{BaseURL: server.URL})
	if err == nil && c != nil {
		_, _ = c.Get(ctx, "/probe", nil)
	}
	if c != nil || session != nil {
		t.Fatalf("failed MCP initialization returned client=%v session=%v", c != nil, session != nil)
	}
	if err == nil || !strings.Contains(err.Error(), "MCP hook failed") {
		t.Fatalf("newMCPClientFromConfig() error = %v, want MCP hook failure", err)
	}
	if requests != 0 {
		t.Fatalf("outbound requests after hook failure = %d, want 0", requests)
	}
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "mcp", "client_hook_contract_test.go"),
		[]byte(mcpTest),
		0o644,
	))

	runGoCommand(t, outputDir, "test", "./internal/mcp", "-run", "^TestMCPClientHookFailurePropagates$", "-count=1")
}
