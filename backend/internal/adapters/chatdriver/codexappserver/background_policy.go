// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).

package codexappserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/chatdriver/codexappserver/codexproto"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// backgroundThreadConfig follows Codex's own temporary structured request:
// codex-rs/tui/src/temporary_structured_request.rs, start_temporary_thread.
// Native config overlays merge maps, so an empty mcp_servers map cannot disable
// inherited servers. Read their effective names and disable each explicitly.
func backgroundThreadConfig(ctx context.Context, connection *conn, cwd string, injected []ports.ChatMCPServerConfig) (map[string]any, error) {
	// Generated Config drops the schema's flattened additional keys. Preserve
	// only the effective MCP map here; server credentials are never copied to
	// the thread override, logs, errors or durable workflow facts.
	var response struct {
		Config json.RawMessage `json:"config"`
	}
	includeLayers := false
	if err := connection.request(ctx, codexproto.MethodConfigRead, codexproto.ConfigReadParams{Cwd: &cwd, IncludeLayers: &includeLayers}, &response); err != nil {
		return nil, fmt.Errorf("read effective background tool configuration: %w", err)
	}
	var config map[string]json.RawMessage
	if json.Unmarshal(response.Config, &config) != nil || config == nil {
		return nil, fmt.Errorf("effective background tool configuration is unavailable")
	}
	servers := map[string]any{}
	if raw, found := config["mcp_servers"]; found {
		var configured map[string]json.RawMessage
		if json.Unmarshal(raw, &configured) != nil || configured == nil {
			return nil, fmt.Errorf("effective background MCP configuration is unavailable")
		}
		for name := range configured {
			servers[name] = map[string]any{"enabled": false}
		}
	}
	for _, server := range injected {
		servers[server.Name] = map[string]any{"enabled": false}
	}
	// These are native per-thread configuration overrides, not settings writes.
	// Disable external/plugin tools and the local tool families as Codex does
	// for its own text-only temporary structured requests.
	overrides := map[string]any{
		"mcp_servers":                 servers,
		"web_search":                  "disabled",
		"cloud.skills.enabled":        false,
		"skills.include_instructions": false,
		"tools.experimental_request_user_input.enabled": false,
		"tools.update_plan.enabled":                     false,
	}
	for _, feature := range []string{
		"apps", "code_mode", "code_mode_only", "context_management",
		"current_time_reminder", "deferred_executor", "enable_fanout", "goals",
		"hooks", "image_generation", "memories", "multi_agent", "multi_agent_v2",
		"plugins", "request_permissions_tool", "shell_snapshot", "shell_tool",
		"standalone_web_search", "token_budget", "tool_suggest", "unified_exec", "view_image",
	} {
		overrides["features."+feature] = false
	}
	return overrides, nil
}
