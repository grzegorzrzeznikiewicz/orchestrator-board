// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).

package codexappserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestBackgroundThreadDisablesInheritedMCPAndToolDefaults(t *testing.T) {
	d, srv := newTestDriver(t)
	srv.reply("config/read", `{"config":{"mcp_servers":{"preapproved":{"default_tools_approval_mode":"approve","env":{"SECRET":"PRIVATE"}},"disabled":{"enabled":false}},"features":{"plugins":true}}}`)
	cfg := ports.ChatStartConfig{WorkspacePath: "/tmp/background", Ephemeral: true, ReadOnly: true, DisableNetwork: true,
		MCPServers: []ports.ChatMCPServerConfig{{Name: "ao", Type: "stdio", Command: "/PRIVATE/write-tool"}}}
	conv, err := d.Start(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer conv.Close()
	read := srv.awaitFrame(func(f frame) bool { return f.Method == "config/read" })
	var readParams map[string]any
	if err := json.Unmarshal(read.Params, &readParams); err != nil {
		t.Fatal(err)
	}
	if readParams["cwd"] != cfg.WorkspacePath || readParams["includeLayers"] != false {
		t.Fatalf("incorrect effective config scope: %+v", readParams)
	}
	start := srv.awaitFrame(func(f frame) bool { return f.Method == "thread/start" })
	var params map[string]any
	if err := json.Unmarshal(start.Params, &params); err != nil {
		t.Fatal(err)
	}
	config, ok := params["config"].(map[string]any)
	if !ok {
		t.Fatal("background thread inherited unrestricted tool configuration")
	}
	for _, key := range []string{"features.apps", "features.plugins", "features.hooks", "features.shell_tool", "features.unified_exec", "features.multi_agent", "features.multi_agent_v2", "features.request_permissions_tool", "features.image_generation", "features.view_image"} {
		if config[key] != false {
			t.Fatalf("background capability %s is not disabled", key)
		}
	}
	if config["web_search"] != "disabled" {
		t.Fatal("background web search remains available")
	}
	servers, ok := config["mcp_servers"].(map[string]any)
	if !ok || len(servers) != 3 {
		t.Fatalf("not all effective/injected MCP servers disabled: %+v", servers)
	}
	for _, name := range []string{"preapproved", "disabled", "ao"} {
		entry, ok := servers[name].(map[string]any)
		if !ok || len(entry) != 1 || entry["enabled"] != false {
			t.Fatalf("inherited MCP capability retained for %s: %+v", name, entry)
		}
	}
	if strings.Contains(string(start.Params), "PRIVATE") || strings.Contains(string(start.Params), "approve\"") {
		t.Fatal("private MCP configuration or preapproval copied to background thread")
	}
}

func TestBackgroundThreadIsolationFailsClosedBeforeStart(t *testing.T) {
	for name, response := range map[string]string{"missing config": `{}`, "null config": `{"config":null}`, "wrong config": `{"config":[]}`, "invalid MCP shape": `{"config":{"mcp_servers":[]}}`, "native RPC refused": ""} {
		t.Run(name, func(t *testing.T) {
			d, srv := newTestDriver(t)
			if response == "" {
				srv.replyError("config/read", -32601, "unsupported")
			} else {
				srv.reply("config/read", response)
			}
			conv, err := d.Start(context.Background(), ports.ChatStartConfig{WorkspacePath: "/tmp/background", Ephemeral: true, ReadOnly: true, DisableNetwork: true})
			if conv != nil {
				defer conv.Close()
			}
			if err == nil || srv.sentMethod("thread/start") || srv.sentMethod("turn/start") {
				t.Fatalf("unverified MCP isolation started work: err=%v", err)
			}
		})
	}
}

func TestBackgroundIsolationDoesNotRestrictOrdinaryReadOnlyReviews(t *testing.T) {
	d, srv := newTestDriver(t)
	conv, err := d.Start(t.Context(), ports.ChatStartConfig{WorkspacePath: "/tmp/review", ReadOnly: true, DisableNetwork: false, MCPServers: []ports.ChatMCPServerConfig{{Name: "ao", Type: "stdio", Command: "ao"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conv.Close()
	if srv.sentMethod("config/read") {
		t.Fatal("ordinary reviewer inherited background-only restrictions")
	}
	start := srv.awaitFrame(func(f frame) bool { return f.Method == "thread/start" })
	if !strings.Contains(string(start.Params), `"command":"ao"`) {
		t.Fatal("ordinary review lost its AO submission tool")
	}
}
