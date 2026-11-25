package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSessionDeletable(t *testing.T) {
	tests := []struct {
		typ  SessionType
		want bool
	}{
		{SessionTypeMain, false},
		{SessionTypeHidden, false},
		{SessionTypeChannel, true},
		{SessionTypeTask, true},
		{SessionTypeUser, true},
		{"", true},
		{"custom", true},
	}
	for _, tt := range tests {
		t.Run(string(tt.typ), func(t *testing.T) {
			s := &Session{Type: tt.typ}
			assert.Equal(t, tt.want, s.Deletable())
		})
	}
}

func TestNewMessageSender(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		senderName  string
		participant bool
		want        *MessageSender
	}{
		{name: "both empty", want: nil},
		{name: "both whitespace", id: "  \t", senderName: "\n ", participant: true, want: nil},
		{name: "id and name", id: "u1", senderName: "Alice", participant: true, want: &MessageSender{ID: "u1", Name: "Alice", Participant: true}},
		{name: "trims whitespace", id: "  u1 ", senderName: "\tAlice\n", want: &MessageSender{ID: "u1", Name: "Alice"}},
		{name: "name falls back to id", id: " u1 ", senderName: "   ", participant: true, want: &MessageSender{ID: "u1", Name: "u1", Participant: true}},
		{name: "name without id", senderName: " Bob ", want: &MessageSender{Name: "Bob"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NewMessageSender(tt.id, tt.senderName, tt.participant))
		})
	}
}
