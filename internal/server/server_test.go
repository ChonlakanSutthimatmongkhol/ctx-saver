package server_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/sandbox"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/server"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// listTools creates a real MCP server, connects a client in-process, and
// returns all registered tools via the tools/list RPC.
func listTools(t *testing.T) []*mcp.Tool {
	t.Helper()

	dir := t.TempDir()
	cfg := config.Default()
	cfg.Storage.DataDir = dir
	cfg.Logging.File = dir + "/server.log"

	st, err := store.NewSQLiteStore(dir, "/test-project")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	sb := sandbox.NewSubprocess(cfg.DenyCommands)
	srv := server.New(cfg, sb, st, "/test-project", dir, time.Now())

	ct, srvTransport := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(context.Background(), srvTransport, nil)
	require.NoError(t, err)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(context.Background(), ct, nil)
	require.NoError(t, err)

	t.Cleanup(func() {
		cs.Close()
		_ = ss.Wait()
	})

	res, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)
	return res.Tools
}

func TestToolDescriptions_NonEmpty(t *testing.T) {
	tools := listTools(t)
	require.NotEmpty(t, tools, "expected at least one tool registered")

	for _, tool := range tools {
		desc := tool.Description
		assert.GreaterOrEqual(t, len(desc), 200,
			"tool %q description too short (%d chars) — must be ≥ 200 chars after Phase 6 rewrite",
			tool.Name, len(desc))
	}
}

func TestToolDescriptions_MentionAlternative(t *testing.T) {
	tools := listTools(t)

	byName := make(map[string]string, len(tools))
	for _, tool := range tools {
		byName[tool.Name] = tool.Description
	}

	execDesc, ok := byName["ctx_execute"]
	require.True(t, ok, "ctx_execute not registered")
	assert.True(t,
		strings.Contains(execDesc, "runInTerminal") ||
			strings.Contains(execDesc, "Shell") ||
			strings.Contains(execDesc, "Bash"),
		"ctx_execute description must mention at least one native tool alternative (runInTerminal/Shell/Bash)")

	readDesc, ok := byName["ctx_read_file"]
	require.True(t, ok, "ctx_read_file not registered")
	assert.True(t,
		strings.Contains(readDesc, "readFile") || strings.Contains(readDesc, "read_file"),
		"ctx_read_file description must mention native alternative (readFile/read_file)")
}

func TestToolDescriptions_NoRegression(t *testing.T) {
	tools := listTools(t)

	expected := []string{
		"ctx_execute",
		"ctx_read_file",
		"ctx_search",
		"ctx_list_outputs",
		"ctx_get_full",
		"ctx_outline",
		"ctx_stats",
		"ctx_get_section",
		// ctx_session_init added in Task 6.3
	}

	byName := make(map[string]bool, len(tools))
	for _, tool := range tools {
		assert.NotEmpty(t, tool.Name, "tool registered with empty Name")
		byName[tool.Name] = true
	}

	for _, name := range expected {
		assert.True(t, byName[name], "expected tool %q to be registered", name)
	}
}
