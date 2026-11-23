package mcp

import (
	"context"
	"mime"
	"path"
	"sort"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/scheduler"
	"github.com/lsegal/aviary/internal/sessiontarget"
	"github.com/lsegal/aviary/internal/store"
)

// channelActivityArgs selects the channel channel_activity summarizes.
type channelActivityArgs struct {
	Agent   string `json:"agent"`
	Channel string `json:"channel"`
	Limit   int    `json:"limit,omitempty"`
}

// channelActivity is a channel's recent traffic, for the Channels overview.
type channelActivity struct {
	// LastActivity is when the channel's latest message, from anyone, was
	// sent; empty when it has none.
	LastActivity string `json:"last_activity,omitempty"`
	// Messages24h counts the messages others sent in the last 24 hours.
	Messages24h int `json:"messages_24h"`
	// Messages are the latest messages others sent, oldest first.
	Messages []channelActivityMessage `json:"messages"`
}

// channelActivityMessage is one message someone sent on a channel.
type channelActivityMessage struct {
	Entry string `json:"entry"`
	// Conversation is the title of the conversation the message is on.
	Conversation string `json:"conversation,omitempty"`
	Sender       string `json:"sender,omitempty"`
	Text         string `json:"text"`
	Timestamp    string `json:"timestamp"`
	// Attachments lists each attachment's kind: image, audio, video or file.
	Attachments []string `json:"attachments,omitempty"`
	// Workflows names the workflows that handled the message.
	Workflows []string `json:"workflows,omitempty"`
}

// channelMessage is a message someone sent on one of a channel's
// conversations, with its sender named.
type channelMessage struct {
	Entry        string
	Conversation string
	Sender       string
	Message      domain.Message
}

// channelHistory is everything read from a channel's sessions.
type channelHistory struct {
	// Messages are the messages others sent on the channel's conversations,
	// oldest first.
	Messages []channelMessage
	// LastActivity is the latest message on any of the channel's sessions.
	LastActivity time.Time
}

func registerChannelActivityTool(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "channel_activity",
		Description: "Summarize a channel's recent traffic: when its last message was sent, how many messages others sent in the last 24 hours, and the latest limit messages others sent (default 5, at most 100), oldest first, each with its sender's display name, its attachments' kinds and the workflows that handled it",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args channelActivityArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		_, a, idx, err := loadChannel(args.Agent, args.Channel)
		if err != nil {
			return nil, struct{}{}, err
		}
		limit := args.Limit
		if limit <= 0 || limit > 100 {
			limit = 5
		}
		h, err := readChannelHistory(a, idx, "")
		if err != nil {
			return nil, struct{}{}, err
		}
		out := channelActivity{Messages: []channelActivityMessage{}}
		if !h.LastActivity.IsZero() {
			out.LastActivity = h.LastActivity.Format(time.RFC3339Nano)
		}
		since := time.Now().Add(-24 * time.Hour)
		for _, m := range h.Messages {
			if m.Message.Timestamp.After(since) {
				out.Messages24h++
			}
		}
		msgs := h.Messages
		if len(msgs) > limit {
			msgs = msgs[len(msgs)-limit:]
		}
		for _, m := range msgs {
			am := channelActivityMessage{
				Entry:        m.Entry,
				Conversation: m.Conversation,
				Sender:       m.Sender,
				Text:         m.Message.Content,
				Timestamp:    m.Message.Timestamp.Format(time.RFC3339Nano),
			}
			for _, u := range m.Message.MediaURLs {
				am.Attachments = append(am.Attachments, mediaKind(u))
			}
			for _, w := range m.Message.Workflows {
				am.Workflows = append(am.Workflows, w.Name)
			}
			out.Messages = append(out.Messages, am)
		}
		return jsonResult(out)
	})
}

// readChannelHistory reads the messages others sent on the channel's
// conversations, or only on entry when it is set. Each sender is named by
// the latest display name the channel has seen for their ID, so a message
// that only recorded a raw ID, such as a Signal contact's phone number before
// their profile name arrived, still shows the member's name.
func readChannelHistory(a config.AgentConfig, idx int, entry string) (channelHistory, error) {
	var h channelHistory
	sessions, err := sessiontarget.ChannelSessions(a, idx)
	if err != nil {
		return h, err
	}
	names := map[string]string{}
	named := map[string]time.Time{}
	for _, sess := range sessions {
		path := store.FindSessionPath(sess.AgentID, sess.ID)
		if path == "" {
			continue
		}
		lines, err := store.ReadJSONL[domain.Message](path)
		if err != nil {
			continue
		}
		e := sessionEntry(a.Channels[idx], sess.Name)
		for _, m := range lines {
			if m.Timestamp.After(h.LastActivity) {
				h.LastActivity = m.Timestamp
			}
			if m.Role != domain.MessageRoleUser {
				continue
			}
			if m.Sender != nil {
				id := strings.TrimSpace(m.Sender.ID)
				if name := scheduler.DisplayName(id, m.Sender.Name); name != "" && id != "" && !m.Timestamp.Before(named[id]) {
					names[id], named[id] = name, m.Timestamp
				}
			}
			if e == "" || (entry != "" && e != entry) {
				continue
			}
			h.Messages = append(h.Messages, channelMessage{Entry: e, Conversation: sess.Title, Message: m})
		}
	}
	for i := range h.Messages {
		if s := h.Messages[i].Message.Sender; s != nil {
			h.Messages[i].Sender = names[strings.TrimSpace(s.ID)]
			if h.Messages[i].Sender == "" {
				h.Messages[i].Sender = scheduler.DisplayName(s.ID, s.Name)
			}
		}
	}
	sort.SliceStable(h.Messages, func(i, j int) bool {
		return h.Messages[i].Message.Timestamp.Before(h.Messages[j].Message.Timestamp)
	})
	return h, nil
}

// mediaKind classifies a message's media URL, a data: URL or a file path or
// URL, as image, audio, video or file.
func mediaKind(u string) string {
	mt := ""
	if rest, ok := strings.CutPrefix(u, "data:"); ok {
		mt, _, _ = strings.Cut(rest, ";")
		mt, _, _ = strings.Cut(mt, ",")
	} else {
		p, _, _ := strings.Cut(u, "?")
		mt = mime.TypeByExtension(strings.ToLower(path.Ext(p)))
	}
	switch kind, _, _ := strings.Cut(mt, "/"); kind {
	case "image", "audio", "video":
		return kind
	}
	return "file"
}
