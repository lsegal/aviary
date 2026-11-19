// Package cmd implements the aviary CLI subcommands.
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/projects/transcript"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage agents",
}

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured agents and their current state",
	RunE: func(cmd *cobra.Command, _ []string) error {
		out, err := dispatcher.CallTool(cmd.Context(), "agent_list", nil)
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

var agentRunFile string
var agentRunBare bool
var agentRunHistory bool
var agentRunWorkdir string
var agentRunJSON bool

var agentRunCmd = &cobra.Command{
	Use:   "run <name> [message]",
	Short: "Send a message to an agent and stream the response",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		msg := ""
		if len(args) > 1 {
			msg = args[1]
		}
		if agentRunFile != "" {
			data, err := os.ReadFile(agentRunFile)
			if err != nil {
				return fmt.Errorf("reading file: %w", err)
			}
			msg = string(data)
		}
		if msg == "" {
			return fmt.Errorf("message required: pass as argument or use --file")
		}
		toolArgs := map[string]any{
			"name":    name,
			"message": msg,
			"bare":    agentRunBare,
		}
		if cmd.Flags().Changed("history") {
			toolArgs["history"] = agentRunHistory
		}
		if agentRunWorkdir != "" {
			workdir, err := filepath.Abs(agentRunWorkdir)
			if err != nil {
				return fmt.Errorf("resolving workdir: %w", err)
			}
			toolArgs["workdir"] = workdir
		}
		if agentRunJSON {
			toolArgs["include_tool_progress"] = true
			toolArgs["include_file_progress"] = true
			events := newAgentRunEventWriter(cmd.OutOrStdout())
			out, err := dispatcher.CallToolProgress(cmd.Context(), "agent_run", toolArgs, events.progress)
			if err != nil {
				return err
			}
			return events.finish(out)
		}
		out, err := dispatcher.CallTool(cmd.Context(), "agent_run", toolArgs)
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	},
}

// agentRunEventWriter turns agent_run's progress notifications into the
// transcript events `aviary agent run --json` prints, one per line. Text
// arrives in fragments, so it is buffered and printed as one message before
// the next tool call or file change and at the end of the run.
type agentRunEventWriter struct {
	mu       sync.Mutex
	encoder  *json.Encoder
	text     strings.Builder
	messaged bool
	err      error
}

func newAgentRunEventWriter(w io.Writer) *agentRunEventWriter {
	return &agentRunEventWriter{encoder: json.NewEncoder(w)}
}

func (w *agentRunEventWriter) progress(message string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch {
	case strings.HasPrefix(message, "[tool]"):
		var tool struct {
			Name string         `json:"name"`
			Args map[string]any `json:"args"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(message, "[tool]")), &tool) != nil {
			return
		}
		w.flushText()
		w.write(transcript.Event{Kind: transcript.KindToolCall, Name: tool.Name, Input: tool.Args})
	case strings.HasPrefix(message, "[file]"):
		var file struct {
			Tool string `json:"tool"`
			Path string `json:"path"`
			Diff string `json:"diff"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(message, "[file]")), &file) != nil {
			return
		}
		w.flushText()
		w.write(transcript.Event{Kind: transcript.KindFileChange, Name: file.Tool, Path: file.Path, Diff: file.Diff})
	case strings.HasPrefix(message, "[media]"):
	default:
		w.text.WriteString(message)
	}
}

// finish prints the buffered text, or the run's final reply when no text
// streamed, and reports the first write error.
func (w *agentRunEventWriter) finish(reply string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushText()
	if !w.messaged && strings.TrimSpace(reply) != "" {
		w.write(transcript.Event{Kind: transcript.KindMessage, Text: reply})
	}
	return w.err
}

func (w *agentRunEventWriter) flushText() {
	text := w.text.String()
	w.text.Reset()
	if strings.TrimSpace(text) == "" {
		return
	}
	w.messaged = true
	w.write(transcript.Event{Kind: transcript.KindMessage, Text: text})
}

func (w *agentRunEventWriter) write(event transcript.Event) {
	event.Time = time.Now()
	if err := w.encoder.Encode(event); err != nil && w.err == nil {
		w.err = err
	}
}

var agentStopCmd = &cobra.Command{
	Use:   "stop <name>",
	Short: "Immediately stop all work in progress for an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := dispatcher.CallTool(cmd.Context(), "agent_stop", map[string]any{
			"name": args[0],
		})
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

var agentTemplateSyncCmd = &cobra.Command{
	Use:   "template-sync <name>",
	Short: "Sync embedded template files into an agent directory",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := dispatcher.CallTool(cmd.Context(), "agent_template_sync", map[string]any{
			"agent": args[0],
		})
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

func init() {
	agentRunCmd.Flags().StringVar(&agentRunFile, "file", "", "read prompt from file")
	agentRunCmd.Flags().BoolVar(&agentRunBare, "bare", false, "run without any system prompt, rules, memory, or tool preamble")
	agentRunCmd.Flags().BoolVar(&agentRunHistory, "history", true, "include prior session history in the prompt")
	agentRunCmd.Flags().StringVar(&agentRunWorkdir, "workdir", "", "grant the run's file tools and exec this directory for its duration, e.g. a job's clone")
	agentRunCmd.Flags().BoolVar(&agentRunJSON, "json", false, "print the run as transcript events, one JSON object per line")
	agentCmd.AddCommand(agentListCmd, agentRunCmd, agentStopCmd, agentTemplateSyncCmd)
	rootCmd.AddCommand(agentCmd)
}
