package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToolCallIDs_ID(t *testing.T) {
	call := func(name string) *ToolEvent { return &ToolEvent{Name: name} }
	done := func(name string) *ToolEvent { return &ToolEvent{Name: name, Done: true} }

	tests := []struct {
		name   string
		events []*ToolEvent
		want   []string
	}{
		{
			name:   "backend IDs pass through",
			events: []*ToolEvent{{ID: "toolu_a", Name: "read"}, {ID: "toolu_a", Name: "read", Done: true}},
			want:   []string{"toolu_a", "toolu_a"},
		},
		{
			name:   "calls without IDs are numbered",
			events: []*ToolEvent{call("read"), call("write"), call("read")},
			want:   []string{"call_1", "call_2", "call_3"},
		},
		{
			name:   "results pair FIFO per tool",
			events: []*ToolEvent{call("read"), call("read"), done("read"), done("read")},
			want:   []string{"call_1", "call_2", "call_1", "call_2"},
		},
		{
			name:   "tools queue independently",
			events: []*ToolEvent{call("read"), call("write"), done("write"), call("read"), done("read"), done("read")},
			want:   []string{"call_1", "call_2", "call_2", "call_3", "call_1", "call_3"},
		},
		{
			name:   "result without pending call on zero value",
			events: []*ToolEvent{done("read")},
			want:   []string{""},
		},
		{
			name:   "result after queue drained or for other tool",
			events: []*ToolEvent{call("read"), done("write"), done("read"), done("read")},
			want:   []string{"call_1", "", "call_1", ""},
		},
		{
			name:   "backend IDs do not consume numbers or queue entries",
			events: []*ToolEvent{{ID: "toolu_a", Name: "read"}, call("read"), {ID: "toolu_a", Name: "read", Done: true}, done("read")},
			want:   []string{"toolu_a", "call_1", "toolu_a", "call_1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ids ToolCallIDs
			got := make([]string, len(tt.events))
			for i, ev := range tt.events {
				got[i] = ids.ID(ev)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
