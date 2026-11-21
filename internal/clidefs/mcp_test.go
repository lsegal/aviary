package clidefs

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestBuiltinGeminiTakesMCPAndSystemPrompt pins how a gemini chat is handed
// Aviary's MCP server, through a system settings file whose header expands
// the token variable and whose policy file allows the server's tools in plan
// mode, and the system prompt, through GEMINI_SYSTEM_MD.
func TestBuiltinGeminiTakesMCPAndSystemPrompt(t *testing.T) {
	gemini, ok := MustBuiltin().Lookup("gemini")
	if !ok || !gemini.TakesMCP() || !gemini.TakesSystemPrompt() {
		t.Fatalf("gemini declares mcp %t and systemPrompt %t", gemini.TakesMCP(), gemini.TakesSystemPrompt())
	}
	values := MCPValues{Name: "aviary", URL: "http://127.0.0.1:1/mcp", TokenEnv: "TOK", AuthEnv: "AUTH", Timeout: 30 * time.Minute, File: "/tmp/s.json", AllowFile: "/tmp/p.toml"}
	if got := gemini.MCPEnv(values); !reflect.DeepEqual(got, []string{"GEMINI_CLI_SYSTEM_SETTINGS_PATH=/tmp/s.json"}) {
		t.Errorf("MCPEnv = %q", got)
	}
	if got := gemini.MCPArgs(values); got != nil {
		t.Errorf("MCPArgs = %q, want none", got)
	}
	data, err := gemini.MCPFile(values)
	if err != nil {
		t.Fatal(err)
	}
	var file map[string]any
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"mcpServers": map[string]any{"aviary": map[string]any{
		"httpUrl": "http://127.0.0.1:1/mcp",
		"headers": map[string]any{"Authorization": "Bearer ${TOK}"},
		"trust":   true,
		"timeout": float64(1800000),
	}}, "policyPaths": []any{"/tmp/p.toml", "~/.gemini/policies"}}
	if !reflect.DeepEqual(file, want) {
		t.Errorf("MCPFile = %s", data)
	}
	if ext, ok := gemini.MCPAllowFileExtension(); !ok || ext != ".toml" {
		t.Errorf("MCPAllowFileExtension = %q, %t", ext, ok)
	}
	policy := "[[rule]]\ntoolName = \"*\"\nmcpName = \"aviary\"\ndecision = \"allow\"\npriority = 100\nmodes = [\"plan\"]\n"
	if got := string(gemini.MCPAllowFile(values)); got != policy {
		t.Errorf("MCPAllowFile = %q, want %q", got, policy)
	}
	if got := gemini.MCPToolPrefix("aviary"); got != "mcp_aviary_" {
		t.Errorf("MCPToolPrefix = %q", got)
	}
	if got := gemini.SystemPromptEnv(SystemPromptValues{Prompt: "p", File: "/tmp/p.md"}); !reflect.DeepEqual(got, []string{"GEMINI_SYSTEM_MD=/tmp/p.md"}) {
		t.Errorf("SystemPromptEnv = %q", got)
	}
}

// TestBuiltinMuseTakesMCPAndSystemPrompt pins how a muse chat is handed
// Aviary's MCP server, through -c settings in a trusted workspace whose header
// is a whole variable, and the system prompt, appended through a -c setting
// whose value is the prompt as a JSON string.
func TestBuiltinMuseTakesMCPAndSystemPrompt(t *testing.T) {
	muse, ok := MustBuiltin().Lookup("muse")
	if !ok || !muse.TakesMCP() || !muse.TakesSystemPrompt() {
		t.Fatalf("muse declares mcp %t and systemPrompt %t", muse.TakesMCP(), muse.TakesSystemPrompt())
	}
	values := MCPValues{Name: "aviary", URL: "http://127.0.0.1:1/mcp", TokenEnv: "TOK", AuthEnv: "AUTH", Timeout: 30 * time.Minute}
	want := []string{
		"--trust-workspace",
		"-c", `mcpServers.aviary.url="http://127.0.0.1:1/mcp"`,
		"-c", `mcpServers.aviary.headers.Authorization="${AUTH}"`,
	}
	if got := muse.MCPArgs(values); !reflect.DeepEqual(got, want) {
		t.Errorf("MCPArgs = %q", got)
	}
	if muse.MCPWritesFile() || len(muse.MCPEnv(values)) > 0 {
		t.Error("muse takes its server as arguments only")
	}
	if got := muse.MCPToolPrefix("aviary"); got != "mcp__aviary__" {
		t.Errorf("MCPToolPrefix = %q", got)
	}
	if muse.SystemPromptWritesFile() {
		t.Error("muse takes the system prompt inline, so no file is written")
	}
	prompt := "Be <brief>.\nSay \"hi\" & stop."
	got := muse.SystemPromptArgs(SystemPromptValues{Prompt: prompt})
	if len(got) != 2 || got[0] != "-c" || !strings.HasPrefix(got[1], "run.system_prompt_append=") {
		t.Fatalf("SystemPromptArgs = %q", got)
	}
	var decoded string
	if err := json.Unmarshal([]byte(strings.TrimPrefix(got[1], "run.system_prompt_append=")), &decoded); err != nil || decoded != prompt {
		t.Errorf("setting value %q decodes to %q (%v), want the prompt", got[1], decoded, err)
	}
	if strings.Contains(got[1], `\u003c`) {
		t.Errorf("setting value %q escapes HTML characters it need not", got[1])
	}
}

// TestBuiltinsWithoutMCP checks a definition that declares neither block
// renders nothing for them.
func TestBuiltinsWithoutMCP(t *testing.T) {
	cline, _ := MustBuiltin().Lookup("cline")
	if cline.TakesMCP() || cline.TakesSystemPrompt() || cline.MCPArgs(MCPValues{URL: "u"}) != nil || cline.SystemPromptEnv(SystemPromptValues{File: "f"}) != nil {
		t.Error("cline renders MCP or system prompt wiring it does not declare")
	}
}

// TestUserDefinitionMCPAndSystemPrompt checks the mcp and systemPrompt blocks
// of a cli_definitions entry are validated, naming the field.
func TestUserDefinitionMCPAndSystemPrompt(t *testing.T) {
	define := func(block string, value any, run ...any) error {
		if len(run) == 0 {
			run = []any{map[string]any{"args": []any{"{approval}", "{prompt}"}}}
		}
		_, err := FromConfig(map[string]any{"mine": map[string]any{
			"binary":  "mine",
			"session": map[string]any{"assign": "none"},
			"output":  map[string]any{"format": "text"},
			block:     value,
			"args":    map[string]any{"run": run},
		}})
		return err
	}
	server := map[string]any{"url": "{mcpUrl}", "auth": "Bearer ${{mcpTokenEnv}}"}
	allowed := map[string]any{"url": "{mcpUrl}", "auth": "Bearer ${{mcpTokenEnv}}", "policies": []any{"{mcpAllowFile}"}}
	allow := map[string]any{"extension": ".toml", "lines": []any{"allow {mcpName}"}}
	valid := []struct {
		block string
		value any
	}{
		{"mcp", map[string]any{"args": []any{"--mcp", "{mcpUrl}", "--token-var", "{mcpTokenEnv}"}, "toolPrefix": "mcp__{mcpName}__"}},
		{"mcp", map[string]any{"env": map[string]any{"SETTINGS": "{mcpFile}"}, "file": server, "toolPrefix": "x_"}},
		{"mcp", map[string]any{"args": []any{"-c", `auth="${{mcpAuthEnv}}"`, "-c", `url="{mcpUrl}"`}, "toolPrefix": "x_"}},
		{"mcp", map[string]any{"env": map[string]any{"SETTINGS": "{mcpFile}"}, "file": allowed, "allowFile": allow, "toolPrefix": "x_"}},
		{"mcp", map[string]any{"args": []any{"--policy", "{mcpAllowFile}", "--mcp", "{mcpUrl}", "{mcpTokenEnv}"}, "allowFile": map[string]any{"lines": []any{"x"}}, "toolPrefix": "x_"}},
		{"systemPrompt", map[string]any{"args": []any{"--system-file", "{systemPromptFile}"}}},
		{"systemPrompt", map[string]any{"env": map[string]any{"PROMPT_FILE": "{systemPromptFile}"}}},
		{"systemPrompt", map[string]any{"args": []any{"-c", "instructions={systemPromptJSON}"}}},
	}
	for _, c := range valid {
		if err := define(c.block, c.value); err != nil {
			t.Errorf("%s %v: %v", c.block, c.value, err)
		}
	}
	invalid := []struct {
		block string
		value any
		want  string
	}{
		{"mcp", map[string]any{"args": []any{"--mcp", "{mcpUrl}", "{mcpTokenEnv}"}}, `"mcp.toolPrefix" is required`},
		{"mcp", map[string]any{"args": []any{"--mcp", "{mcpTokenEnv}"}, "toolPrefix": "x_"}, `{mcpUrl}`},
		{"mcp", map[string]any{"args": []any{"--mcp", "{mcpUrl}"}, "toolPrefix": "x_"}, `bearer token`},
		{"mcp", map[string]any{"args": []any{"{mcpUrl}", "{mcpTokenEnv}", "{prompt}"}, "toolPrefix": "x_"}, `may only use`},
		{"mcp", map[string]any{"args": []any{"{mcpUrl}", "{mcpTokenEnv}"}, "toolPrefix": "{mcpUrl}"}, `"mcp.toolPrefix"`},
		{"mcp", map[string]any{"file": server, "toolPrefix": "x_"}, `never handed to the CLI`},
		{"mcp", map[string]any{"args": []any{"{mcpFile}", "{mcpUrl}", "{mcpTokenEnv}"}, "toolPrefix": "x_"}, `declares no "mcp.file"`},
		{"mcp", map[string]any{"env": map[string]any{"S": "{mcpFile}"}, "file": map[string]any{"self": "{mcpFile}", "u": "{mcpUrl}", "t": "{mcpTokenEnv}"}, "toolPrefix": "x_"}, `own path`},
		{"mcp", map[string]any{"env": map[string]any{"A=B": "{mcpUrl}{mcpTokenEnv}"}, "toolPrefix": "x_"}, `usable variable name`},
		{"mcp", map[string]any{"args": []any{"{mcpUrl}", "{mcpTokenEnv}"}, "allowFile": allow, "toolPrefix": "x_"}, `"mcp.allowFile" is never handed to the CLI`},
		{"mcp", map[string]any{"args": []any{"{mcpAllowFile}", "{mcpUrl}", "{mcpTokenEnv}"}, "toolPrefix": "x_"}, `declares no "mcp.allowFile"`},
		{"mcp", map[string]any{"args": []any{"{mcpAllowFile}", "{mcpUrl}", "{mcpTokenEnv}"}, "allowFile": map[string]any{"lines": []any{}}, "toolPrefix": "x_"}, `"mcp.allowFile.lines" is required`},
		{"mcp", map[string]any{"args": []any{"{mcpAllowFile}", "{mcpUrl}", "{mcpTokenEnv}"}, "allowFile": map[string]any{"extension": "toml", "lines": []any{"x"}}, "toolPrefix": "x_"}, `must start with "."`},
		{"mcp", map[string]any{"args": []any{"{mcpAllowFile}", "{mcpUrl}", "{mcpTokenEnv}"}, "allowFile": map[string]any{"extension": "./x.toml", "lines": []any{"x"}}, "toolPrefix": "x_"}, `name no directory`},
		{"mcp", map[string]any{"args": []any{"{mcpAllowFile}", "{mcpUrl}", "{mcpTokenEnv}"}, "allowFile": map[string]any{"lines": []any{"{mcpFile}"}}, "toolPrefix": "x_"}, `"mcp.allowFile.lines"`},
		{"systemPrompt", map[string]any{"args": []any{"--system"}}, `{systemPromptFile} or pass the prompt with {systemPromptJSON}`},
		{"systemPrompt", map[string]any{"args": []any{"{systemPromptFile}", "{mcpUrl}"}}, `may only use`},
	}
	for _, c := range invalid {
		err := define(c.block, c.value)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %v: error %v, want one containing %q", c.block, c.value, err, c.want)
		}
	}
	noApproval := []any{map[string]any{"args": []any{"{prompt}"}}}
	if err := define("mcp", valid[0].value, noApproval...); err == nil || !strings.Contains(err.Error(), "{approval}") {
		t.Errorf("mcp.args without {approval}: %v", err)
	}
	if err := define("systemPrompt", valid[5].value, noApproval...); err == nil || !strings.Contains(err.Error(), "{approval}") {
		t.Errorf("systemPrompt.args without {approval}: %v", err)
	}
	if err := define("systemPrompt", valid[6].value, noApproval...); err != nil {
		t.Errorf("systemPrompt.env needs no {approval}: %v", err)
	}
}
