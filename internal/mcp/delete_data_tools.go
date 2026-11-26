package mcp

import (
	"context"
	"fmt"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/sessiontarget"
)

type deletePreviewArgs struct {
	Kind  string `json:"kind" jsonschema:"What is being deleted: task (a scheduled task, project task or workflow), channel or agent" schema:"enum=task|channel|agent"`
	Agent string `json:"agent,omitempty" jsonschema:"The owning agent; needed for a channel, and for a task given by bare name when several agents have it"`
	Name  string `json:"name" jsonschema:"The task (or <agent>/<task>), channel or agent name"`
}

func registerDeleteDataTools(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "delete_preview",
		Description: "Count what deleting a task, workflow, channel or agent with delete_data=true removes: sessions, messages, run records and files. Deletes nothing",
		Annotations: readOnlyTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args deletePreviewArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		counts, err := deletePreview(args)
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(counts)
	})
}

func deletePreview(args deletePreviewArgs) (sessiontarget.DataCounts, error) {
	switch strings.ToLower(strings.TrimSpace(args.Kind)) {
	case "task", "workflow":
		_, agentCfg, task, err := findConfiguredTask(args.Agent, args.Name)
		if err != nil {
			return sessiontarget.DataCounts{}, err
		}
		return sessiontarget.TaskData(agentCfg.Name, task.Name), nil
	case "channel":
		_, a, idx, err := loadChannel(args.Agent, args.Name)
		if err != nil {
			return sessiontarget.DataCounts{}, err
		}
		return sessiontarget.ChannelData(a, idx)
	case "agent":
		cfg, err := config.Load("")
		if err != nil {
			return sessiontarget.DataCounts{}, err
		}
		for _, a := range cfg.Agents {
			if a.Name == strings.TrimSpace(args.Name) {
				return sessiontarget.AgentData(a), nil
			}
		}
		return sessiontarget.DataCounts{}, fmt.Errorf("agent %q not found", args.Name)
	}
	return sessiontarget.DataCounts{}, fmt.Errorf("kind must be task, channel or agent")
}
