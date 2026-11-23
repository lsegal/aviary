package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"

	"github.com/lsegal/aviary/internal/config"
)

// minimalMode serves only Code Loop and Scheduled Tasks, as an `aviary watch`
// session does: every other page's API is refused, not just hidden in the
// web UI.
var minimalMode atomic.Bool

// webUIOff stops the server from serving the web UI at all.
var webUIOff atomic.Bool

// SetMinimalMode turns the minimal Code Loop-only mode on or off for the
// servers created after the call.
func SetMinimalMode(on bool) { minimalMode.Store(on) }

// MinimalMode reports whether the minimal Code Loop-only mode is on.
func MinimalMode() bool { return minimalMode.Load() }

// SetWebUIEnabled controls whether the servers created after the call serve
// the web UI. The MCP endpoint is served either way.
func SetWebUIEnabled(on bool) { webUIOff.Store(!on) }

// minimalTools are the MCP tools a minimal-mode server answers over HTTP:
// what the Code Loop boards, their settings, and Scheduled Tasks call. Chat,
// agents, channels, providers, files, browser control and the instance's own
// pages are refused. Agents' own tool calls go through their in-process MCP
// server, which this does not restrict.
var minimalTools = map[string]bool{
	"ping":          true,
	"server_status": true,
	"agent_list":    true,
	"agent_get":     true,
	"models_list":   true,
	"skills_list":   true,
	"usage_query":   true,
	// provider_cli_list names the CLI providers Code Loop jobs can run on.
	"provider_cli_list": true,
	"config_get":        true,
	"config_validate":   true,
	// config_save is limited to Code Loop settings and tasks; see
	// checkMinimalConfigSave.
	"config_save":                   true,
	"config_task_rename":            true,
	"config_task_convert_to_script": true,
	"config_task_move_to_file":      true,

	"project_status":               true,
	"project_check":                true,
	"project_refresh":              true,
	"project_job_list":             true,
	"project_job_get":              true,
	"project_job_explain":          true,
	"project_job_retry":            true,
	"project_job_stop":             true,
	"project_job_pause":            true,
	"project_job_resume":           true,
	"project_job_dismiss":          true,
	"project_unblock_run":          true,
	"project_clone_list":           true,
	"project_clone_clear":          true,
	"project_github_signin":        true,
	"project_github_use_polling":   true,
	"project_github_import":        true,
	"project_issue_comment":        true,
	"project_issue_backfill_media": true,

	"job_list":    true,
	"job_logs":    true,
	"job_query":   true,
	"job_stop":    true,
	"job_run_now": true,

	"task_list":          true,
	"task_upcoming":      true,
	"task_run":           true,
	"task_stop":          true,
	"task_update":        true,
	"task_delete":        true,
	"task_schedule":      true,
	"task_preview":       true,
	"task_session":       true,
	"task_analyze":       true,
	"task_compile_get":   true,
	"task_compile_query": true,
	"workflow_entries":   true,
	"workflow_preview":   true,

	// A task's dashboard shows the session its runs share, read-only.
	"session_list":     true,
	"session_messages": true,
	"session_history":  true,
	"session_turn":     true,
	"session_stop":     true,
}

// minimalToolAllowed reports whether a minimal-mode server answers a call
// of the tool over HTTP.
func minimalToolAllowed(name string) bool { return minimalTools[name] }

// errMinimalBlocked is the reason a minimal-mode request is refused.
var errMinimalBlocked = errors.New("not available in a watch session")

// toolCall is one JSON-RPC request in an MCP POST body.
type toolCall struct {
	Method string `json:"method"`
	Params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"params"`
}

// minimalToolCalls reads the tool calls in an MCP POST body, which is one
// JSON-RPC request or a batch of them.
func minimalToolCalls(body []byte) ([]toolCall, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, nil
	}
	var calls []toolCall
	if body[0] == '[' {
		if err := json.Unmarshal(body, &calls); err != nil {
			return nil, err
		}
	} else {
		var call toolCall
		if err := json.Unmarshal(body, &call); err != nil {
			return nil, err
		}
		calls = []toolCall{call}
	}
	return calls, nil
}

// checkMinimalToolCalls refuses a body calling a tool minimal mode does not
// answer, or saving config beyond what Code Loop and tasks own.
func checkMinimalToolCalls(body []byte) error {
	calls, err := minimalToolCalls(body)
	if err != nil {
		// The MCP handler rejects what it cannot parse itself.
		return nil
	}
	for _, call := range calls {
		if call.Method != "tools/call" {
			continue
		}
		if !minimalToolAllowed(call.Params.Name) {
			return fmt.Errorf("tool %q is %w", call.Params.Name, errMinimalBlocked)
		}
		if call.Params.Name == "config_save" {
			if err := checkMinimalConfigSave(call.Params.Arguments); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkMinimalConfigSave allows a config_save only when everything but the
// Code Loop settings, the approval mode and the agents' tasks and projects
// is unchanged: a watch session never adds agents, channels, providers or
// server settings.
func checkMinimalConfigSave(raw json.RawMessage) error {
	var args struct {
		Config string `json:"config"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return fmt.Errorf("config_save arguments: %w", err)
	}
	var next config.Config
	if err := json.Unmarshal([]byte(args.Config), &next); err != nil {
		// config_save reports invalid JSON itself.
		return nil
	}
	prev, err := config.Load("")
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if !reflect.DeepEqual(minimalFixedConfig(prev), minimalFixedConfig(&next)) {
		return fmt.Errorf("only Code Loop settings and scheduled tasks can be changed: config_save is otherwise %w", errMinimalBlocked)
	}
	return nil
}

// minimalFixedConfig is the part of cfg a watch session may not change, in
// a form that compares equal however the web UI round-trips it.
func minimalFixedConfig(cfg *config.Config) any {
	fixed := *cfg
	fixed.Projects = config.ProjectsConfig{}
	fixed.Approval = config.ApprovalConfig{}
	fixed.Agents = make([]config.AgentConfig, len(cfg.Agents))
	for i, agent := range cfg.Agents {
		agent.Tasks = nil
		agent.Projects = nil
		fixed.Agents[i] = agent
	}
	data, _ := json.Marshal(fixed)
	var out any
	_ = json.Unmarshal(data, &out)
	return out
}

// minimalMCPHandler refuses the MCP calls minimal mode does not answer.
func minimalMCPHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.Body != nil {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "reading request", http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			if err := checkMinimalToolCalls(body); err != nil {
				http.Error(w, err.Error(), http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// minimalBlockedHandler answers an API minimal mode does not serve.
func minimalBlockedHandler(w http.ResponseWriter, r *http.Request) {
	http.Error(w, strings.TrimPrefix(r.URL.Path, "/")+" is "+errMinimalBlocked.Error(), http.StatusForbidden)
}

// webUIOffHandler answers web UI requests when the web UI is turned off.
func webUIOffHandler(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "the web UI is turned off for this session", http.StatusNotFound)
}
