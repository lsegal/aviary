package clidefs

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// MCP says how an interactive chat hands the CLI Aviary's MCP server: the
// arguments, environment and settings file that register an HTTP server
// whose bearer token the CLI reads from an environment variable. A chat on a
// definition that declares one is offered Aviary's tools the way claude and
// codex chats are; one that declares none runs on the CLI's own tools alone.
//
// Nothing here ever names the token itself, only the variable that holds
// it, so it stays off the command line and out of the settings file.
type MCP struct {
	// Args are added to the chat's run and resume command lines where their
	// {approval} argument stands.
	Args []string `json:"args,omitempty"`
	// Env is added to the chat's environment.
	Env map[string]string `json:"env,omitempty"`
	// File, when declared, is a JSON document written to a temporary file
	// for each turn, whose path {mcpFile} stands for in Args and Env. Its
	// strings take the same placeholders; a string that is exactly
	// {mcpTimeoutMs} or {mcpTimeoutSec} is written as a number.
	File map[string]any `json:"file,omitempty"`
	// ToolPrefix is how the CLI's output names a tool served by the
	// registered server, such as "mcp_{mcpName}_". Those calls are reported
	// by Aviary's MCP boundary, so the chat leaves them out of what it maps
	// from the CLI's output rather than reporting them twice.
	ToolPrefix string `json:"toolPrefix"`
}

// SystemPrompt says how an interactive chat hands the CLI the agent's system
// prompt: written to a temporary file for each turn, whose path
// {systemPromptFile} stands for, or inline as a JSON string, which
// {systemPromptJSON} stands for. A definition that declares none gets the
// prompt at the start of the session's first turn instead.
type SystemPrompt struct {
	// Args are added to the chat's run and resume command lines where their
	// {approval} argument stands.
	Args []string `json:"args,omitempty"`
	// Env is added to the chat's environment.
	Env map[string]string `json:"env,omitempty"`
}

// Placeholders the mcp and systemPrompt blocks may use.
const (
	placeholderMCPName       = "mcpName"
	placeholderMCPURL        = "mcpUrl"
	placeholderMCPTokenEnv   = "mcpTokenEnv"
	placeholderMCPAuthEnv    = "mcpAuthEnv"
	placeholderMCPTimeoutMs  = "mcpTimeoutMs"
	placeholderMCPTimeoutSec = "mcpTimeoutSec"
	placeholderMCPFile       = "mcpFile"
	placeholderSystemPrompt  = "systemPromptFile"
	placeholderSystemJSON    = "systemPromptJSON"
)

var systemPromptPlaceholders = []string{placeholderSystemPrompt, placeholderSystemJSON}

var mcpPlaceholders = []string{
	placeholderMCPName, placeholderMCPURL, placeholderMCPTokenEnv, placeholderMCPAuthEnv,
	placeholderMCPTimeoutMs, placeholderMCPTimeoutSec, placeholderMCPFile,
}

// MCPValues are what one chat substitutes into the definition's mcp block.
type MCPValues struct {
	// Name is the name the server is registered under.
	Name string
	// URL is Aviary's scoped MCP endpoint.
	URL string
	// TokenEnv names the variable holding the bearer token, and AuthEnv the
	// one holding the whole "Bearer <token>" header value, for a CLI that
	// only expands a variable standing for a whole value.
	TokenEnv, AuthEnv string
	// Timeout is how long the CLI waits on one tool call.
	Timeout time.Duration
	// File is the path the rendered settings file was written to.
	File string
}

func (v MCPValues) value(name string) (string, bool) {
	switch name {
	case placeholderMCPName:
		return v.Name, true
	case placeholderMCPURL:
		return v.URL, true
	case placeholderMCPTokenEnv:
		return v.TokenEnv, true
	case placeholderMCPAuthEnv:
		return v.AuthEnv, true
	case placeholderMCPTimeoutMs:
		return strconv.FormatInt(v.Timeout.Milliseconds(), 10), true
	case placeholderMCPTimeoutSec:
		return strconv.FormatInt(int64(v.Timeout/time.Second), 10), true
	case placeholderMCPFile:
		return v.File, true
	}
	return "", false
}

// TakesMCP reports whether the definition declares how to hand its CLI
// Aviary's MCP server.
func (d Definition) TakesMCP() bool { return d.MCP != nil }

// TakesSystemPrompt reports whether the definition declares how to hand its
// CLI a system prompt.
func (d Definition) TakesSystemPrompt() bool { return d.SystemPrompt != nil }

// MCPWritesFile reports whether the definition's mcp block declares a
// settings file.
func (d Definition) MCPWritesFile() bool { return d.MCP != nil && d.MCP.File != nil }

// MCPArgs renders the mcp block's arguments.
func (d Definition) MCPArgs(values MCPValues) []string {
	if d.MCP == nil {
		return nil
	}
	return substituteAll(d.MCP.Args, values.value)
}

// MCPEnv renders the mcp block's environment as KEY=VALUE pairs.
func (d Definition) MCPEnv(values MCPValues) []string {
	if d.MCP == nil {
		return nil
	}
	return renderEnv(d.MCP.Env, values.value)
}

// MCPFile renders the mcp block's settings file, or nothing when it declares
// none.
func (d Definition) MCPFile(values MCPValues) ([]byte, error) {
	if d.MCP == nil || d.MCP.File == nil {
		return nil, nil
	}
	return json.MarshalIndent(renderJSON(d.MCP.File, values.value), "", "  ")
}

// MCPToolPrefix renders the prefix the CLI's output gives a tool served by
// the server registered under name.
func (d Definition) MCPToolPrefix(name string) string {
	if d.MCP == nil {
		return ""
	}
	return substituteAll([]string{d.MCP.ToolPrefix}, MCPValues{Name: name}.value)[0]
}

// SystemPromptValues are what one chat substitutes into the definition's
// systemPrompt block.
type SystemPromptValues struct {
	// Prompt is the system prompt itself.
	Prompt string
	// File is the path the prompt was written to.
	File string
}

func (v SystemPromptValues) value(name string) (string, bool) {
	switch name {
	case placeholderSystemPrompt:
		return v.File, true
	case placeholderSystemJSON:
		var b strings.Builder
		encoder := json.NewEncoder(&b)
		encoder.SetEscapeHTML(false)
		_ = encoder.Encode(v.Prompt)
		return strings.TrimSuffix(b.String(), "\n"), true
	}
	return "", false
}

// SystemPromptWritesFile reports whether the definition's systemPrompt block
// names the prompt file, so the prompt has to be written to one.
func (d Definition) SystemPromptWritesFile() bool {
	if d.SystemPrompt == nil {
		return false
	}
	used, _ := placeholdersIn("systemPrompt", d.systemPromptStrings(), systemPromptPlaceholders)
	return used[placeholderSystemPrompt]
}

// SystemPromptArgs renders the systemPrompt block's arguments.
func (d Definition) SystemPromptArgs(values SystemPromptValues) []string {
	if d.SystemPrompt == nil {
		return nil
	}
	return substituteAll(d.SystemPrompt.Args, values.value)
}

// SystemPromptEnv renders the systemPrompt block's environment.
func (d Definition) SystemPromptEnv(values SystemPromptValues) []string {
	if d.SystemPrompt == nil {
		return nil
	}
	return renderEnv(d.SystemPrompt.Env, values.value)
}

func (d Definition) systemPromptStrings() []string {
	return append(append([]string{}, d.SystemPrompt.Args...), envStrings(d.SystemPrompt.Env)...)
}

func substituteAll(args []string, value func(string) (string, bool)) []string {
	if args == nil {
		return nil
	}
	out := make([]string, len(args))
	for i, arg := range args {
		out[i] = placeholderPattern.ReplaceAllStringFunc(arg, func(match string) string {
			if v, ok := value(strings.Trim(match, "{}")); ok {
				return v
			}
			return match
		})
	}
	return out
}

func renderEnv(env map[string]string, value func(string) (string, bool)) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+substituteAll([]string{env[key]}, value)[0])
	}
	return out
}

// renderJSON substitutes placeholders through a decoded JSON document. A
// string that is exactly a timeout placeholder becomes a number, since
// settings files want their timeouts as numbers.
func renderJSON(node any, value func(string) (string, bool)) any {
	switch node := node.(type) {
	case map[string]any:
		out := make(map[string]any, len(node))
		for key, child := range node {
			out[substituteAll([]string{key}, value)[0]] = renderJSON(child, value)
		}
		return out
	case []any:
		out := make([]any, len(node))
		for i, child := range node {
			out[i] = renderJSON(child, value)
		}
		return out
	case string:
		if node == "{"+placeholderMCPTimeoutMs+"}" || node == "{"+placeholderMCPTimeoutSec+"}" {
			if v, ok := value(strings.Trim(node, "{}")); ok {
				if n, err := strconv.ParseInt(v, 10, 64); err == nil {
					return n
				}
			}
		}
		return substituteAll([]string{node}, value)[0]
	}
	return node
}

// jsonStrings collects every key and string value in a decoded JSON
// document, for validation.
func jsonStrings(node any, out []string) []string {
	switch node := node.(type) {
	case map[string]any:
		for key, child := range node {
			out = jsonStrings(child, append(out, key))
		}
	case []any:
		for _, child := range node {
			out = jsonStrings(child, out)
		}
	case string:
		out = append(out, node)
	}
	return out
}

// placeholdersIn returns the {name} placeholders the strings use, failing on
// one not in allowed.
func placeholdersIn(field string, values []string, allowed []string) (map[string]bool, error) {
	used := map[string]bool{}
	for _, s := range values {
		for _, match := range placeholderPattern.FindAllStringSubmatch(s, -1) {
			if !contains(allowed, match[1]) {
				return nil, fmt.Errorf(`field %q: %q may only use {%s}`, field, s, strings.Join(allowed, "}, {"))
			}
			used[match[1]] = true
		}
	}
	return used, nil
}

func envStrings(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for _, value := range env {
		out = append(out, value)
	}
	return out
}

func validateEnvNames(field string, env map[string]string) error {
	for key := range env {
		if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "= ") {
			return fmt.Errorf(`field %q: %q is not a usable variable name`, field, key)
		}
	}
	return nil
}

func (d Definition) validateMCP() error {
	m := d.MCP
	if m == nil {
		return nil
	}
	if d.InProcess {
		return fmt.Errorf(`field "mcp" is only meaningful when "inProcess" is false`)
	}
	if err := validateEnvNames("mcp.env", m.Env); err != nil {
		return err
	}
	if len(m.Args) > 0 && !d.chatTakesApproval() {
		return fmt.Errorf(`field "mcp.args" needs an argument that is exactly {approval} in "args.run" and "args.resume"`)
	}
	if strings.TrimSpace(m.ToolPrefix) == "" {
		return fmt.Errorf(`field "mcp.toolPrefix" is required`)
	}
	if _, err := placeholdersIn("mcp.toolPrefix", []string{m.ToolPrefix}, []string{placeholderMCPName}); err != nil {
		return err
	}
	direct, err := placeholdersIn("mcp", append(append([]string{}, m.Args...), envStrings(m.Env)...), mcpPlaceholders)
	if err != nil {
		return err
	}
	fileUsed, err := placeholdersIn("mcp.file", jsonStrings(m.File, nil), mcpPlaceholders)
	if err != nil {
		return err
	}
	if fileUsed[placeholderMCPFile] {
		return fmt.Errorf(`field "mcp.file" cannot name its own path with {%s}`, placeholderMCPFile)
	}
	if m.File != nil && !direct[placeholderMCPFile] {
		return fmt.Errorf(`field "mcp.file" is never handed to the CLI: name its path with {%s} in "mcp.args" or "mcp.env"`, placeholderMCPFile)
	}
	if m.File == nil && direct[placeholderMCPFile] {
		return fmt.Errorf(`field "mcp" uses {%s} but declares no "mcp.file"`, placeholderMCPFile)
	}
	all := map[string]bool{}
	for name := range direct {
		all[name] = true
	}
	for name := range fileUsed {
		all[name] = true
	}
	if !all[placeholderMCPURL] {
		return fmt.Errorf(`field "mcp" must name the server's URL with {%s}`, placeholderMCPURL)
	}
	if !all[placeholderMCPTokenEnv] && !all[placeholderMCPAuthEnv] {
		return fmt.Errorf(`field "mcp" must pass the bearer token through the variable {%s} or {%s} names`, placeholderMCPTokenEnv, placeholderMCPAuthEnv)
	}
	return nil
}

func (d Definition) validateSystemPrompt() error {
	s := d.SystemPrompt
	if s == nil {
		return nil
	}
	if d.InProcess {
		return fmt.Errorf(`field "systemPrompt" is only meaningful when "inProcess" is false`)
	}
	if err := validateEnvNames("systemPrompt.env", s.Env); err != nil {
		return err
	}
	if len(s.Args) > 0 && !d.chatTakesApproval() {
		return fmt.Errorf(`field "systemPrompt.args" needs an argument that is exactly {approval} in "args.run" and "args.resume"`)
	}
	used, err := placeholdersIn("systemPrompt", d.systemPromptStrings(), systemPromptPlaceholders)
	if err != nil {
		return err
	}
	if !used[placeholderSystemPrompt] && !used[placeholderSystemJSON] {
		return fmt.Errorf(`field "systemPrompt" must name the prompt file with {%s} or pass the prompt with {%s}`, placeholderSystemPrompt, placeholderSystemJSON)
	}
	return nil
}

// chatTakesApproval reports whether both templates a chat renders have an
// {approval} argument for the chat's own arguments to stand in.
func (d Definition) chatTakesApproval() bool {
	return hasArg(d.Args.Run, "{"+placeholderApproval+"}") && hasArg(d.resumeFragments(), "{"+placeholderApproval+"}")
}

func hasArg(fragments []Fragment, arg string) bool {
	for _, fragment := range fragments {
		if contains(fragment.Args, arg) {
			return true
		}
	}
	return false
}
