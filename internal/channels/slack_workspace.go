package channels

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/slack-go/slack"
)

// SlackWorkspaceChannel describes a Slack channel that the configured bot can see.
type SlackWorkspaceChannel struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	NameNormalized string `json:"name_normalized,omitempty"`
	IsPrivate      bool   `json:"is_private,omitempty"`
	IsMember       bool   `json:"is_member,omitempty"`
	IsArchived     bool   `json:"is_archived,omitempty"`
	NumMembers     int    `json:"num_members,omitempty"`
}

// SlackWorkspaceInfo captures basic workspace identity plus visible channels.
type SlackWorkspaceInfo struct {
	TeamID    string                  `json:"team_id,omitempty"`
	TeamName  string                  `json:"team_name,omitempty"`
	BotUserID string                  `json:"bot_user_id,omitempty"`
	Channels  []SlackWorkspaceChannel `json:"channels"`
}

// ListSlackWorkspaceChannels validates the bot token and lists visible Slack channels.
func ListSlackWorkspaceChannels(ctx context.Context, botToken string) (*SlackWorkspaceInfo, error) {
	botToken = strings.TrimSpace(botToken)
	if botToken == "" {
		return nil, fmt.Errorf("slack bot token is required")
	}
	return listSlackWorkspaceChannelsWithClient(ctx, slack.New(botToken))
}

func listSlackWorkspaceChannelsWithClient(ctx context.Context, client *slack.Client) (*SlackWorkspaceInfo, error) {
	if client == nil {
		return nil, fmt.Errorf("slack client is required")
	}

	authResp, err := client.AuthTestContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("slack auth.test failed: %w", err)
	}

	conversations, err := client.GetAllConversationsContext(
		ctx,
		slack.GetConversationsOptionTypes([]string{"public_channel", "private_channel"}),
		slack.GetConversationsOptionExcludeArchived(true),
		slack.GetConversationsOptionLimit(200),
	)
	if err != nil {
		return nil, fmt.Errorf("slack conversations.list failed: %w", err)
	}

	result := &SlackWorkspaceInfo{
		TeamID:    strings.TrimSpace(authResp.TeamID),
		TeamName:  strings.TrimSpace(authResp.Team),
		BotUserID: strings.TrimSpace(authResp.UserID),
		Channels:  make([]SlackWorkspaceChannel, 0, len(conversations)),
	}

	for _, channel := range conversations {
		if strings.TrimSpace(channel.ID) == "" {
			continue
		}
		name := strings.TrimSpace(channel.Name)
		if name == "" {
			name = strings.TrimSpace(channel.NameNormalized)
		}
		if name == "" {
			name = channel.ID
		}
		result.Channels = append(result.Channels, SlackWorkspaceChannel{
			ID:             channel.ID,
			Name:           name,
			NameNormalized: strings.TrimSpace(channel.NameNormalized),
			IsPrivate:      channel.IsPrivate,
			IsMember:       channel.IsMember,
			IsArchived:     channel.IsArchived,
			NumMembers:     channel.NumMembers,
		})
	}

	sort.Slice(result.Channels, func(i, j int) bool {
		left := strings.ToLower(result.Channels[i].Name)
		right := strings.ToLower(result.Channels[j].Name)
		if left == right {
			return result.Channels[i].ID < result.Channels[j].ID
		}
		return left < right
	})

	return result, nil
}
