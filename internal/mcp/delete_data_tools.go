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
	Kind  string `json:"kind" jsonschema:"What is being deleted: task (a scheduled task, project task or workflow, which is also accepted as workflow), channel or agent" schema:"enum=task|workflow|channel|agent"`
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
	return sessiontarget.DataCounts{}, fmt.Errorf("kind must be task, workflow, channel or agent")
}

// dataOwner names a task or agent whose data config_save deletes.
type dataOwner struct {
	Kind  string `json:"kind" jsonschema:"task or agent" schema:"enum=task|agent"`
	Agent string `json:"agent,omitempty" jsonschema:"The task's agent"`
	Name  string `json:"name" jsonschema:"The task or agent name"`
}

// removedOwner is a task or agent a config save removes, as it was before.
type removedOwner struct {
	dataOwner
	agent config.AgentConfig
}

// removedDataOwners lists the tasks and agents in prev that next drops. A
// task counts only when its agent stays, since an agent that goes takes its
// tasks along.
func removedDataOwners(prev, next *config.Config) []removedOwner {
	var out []removedOwner
	for _, pa := range prev.Agents {
		ni := agentIndex(next, pa.Name)
		if ni < 0 {
			out = append(out, removedOwner{dataOwner{Kind: "agent", Name: pa.Name}, pa})
			continue
		}
		kept := map[string]bool{}
		for _, t := range next.Agents[ni].Tasks {
			kept[t.Name] = true
		}
		for _, t := range pa.Tasks {
			if t.Name != "" && !kept[t.Name] {
				out = append(out, removedOwner{dataOwner{Kind: "task", Agent: pa.Name, Name: t.Name}, pa})
			}
		}
	}
	return out
}

// releaseRemoved disposes of the data of each removed task and agent:
// deleting it when deleteData names it, and otherwise keeping it, with a
// task's session kept as a regular chat.
func releaseRemoved(removed []removedOwner, deleteData []dataOwner) error {
	wanted := map[dataOwner]bool{}
	for _, o := range deleteData {
		o.Kind = strings.ToLower(strings.TrimSpace(o.Kind))
		o.Agent, o.Name = strings.TrimSpace(o.Agent), strings.TrimSpace(o.Name)
		if o.Kind == "agent" {
			o.Agent = ""
		}
		wanted[o] = true
	}
	for _, r := range removed {
		var err error
		if r.Kind == "agent" {
			err = sessiontarget.ReleaseAgent(r.agent, wanted[r.dataOwner])
		} else {
			err = sessiontarget.ReleaseTask(r.Agent, r.Name, wanted[r.dataOwner])
		}
		if err != nil {
			return fmt.Errorf("%s %q: %w", r.Kind, r.Name, err)
		}
	}
	return nil
}
