package mcp

import (
	"context"
	"errors"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/notify"
	"github.com/lsegal/aviary/internal/store"
)

type notifyArgs struct {
	Title    string `json:"title"`
	Body     string `json:"body,omitempty"`
	Severity string `json:"severity,omitempty"`
	Link     string `json:"link,omitempty"`
}

type notificationSettingsSetArgs struct {
	// Settings is the full settings object, as notification_settings_get
	// returns it; reset restores the defaults instead.
	Settings *store.NotificationPreferences `json:"settings,omitempty"`
	Reset    bool                           `json:"reset,omitempty"`
}

var errNoNotifier = errors.New("notifications are only available in a running Aviary server")

// registerNotifyTools registers the notification tools (#1215): sending one,
// reading the bell's history, and reading and writing the settings.
func registerNotifyTools(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name: "notify",
		Description: "Send the user a notification through Aviary's notification system: the web UI's bell, a banner or toast, a system notification, and sound, as their Settings → Notifications allow. " +
			"title (required) is a short headline; body is optional detail; severity is info (default), success, warning or error; link is an optional web UI path it opens, such as /agents/<agent>/chat/<session>. " +
			"Use it for things the user should know about now, not for routine output.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args notifyArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		n := GetDeps().Notifier
		if n == nil {
			return nil, struct{}{}, errNoNotifier
		}
		if args.Title == "" {
			return nil, struct{}{}, errors.New("title is required")
		}
		note, outcome := n.Emit(notify.Sanitize(notify.Event{Title: args.Title, Body: args.Body, Severity: args.Severity, Link: args.Link}))
		return jsonResult(map[string]any{"outcome": outcome, "notification": note})
	})
	addTool(s, &sdkmcp.Tool{
		Name:        "notification_list",
		Description: "List the recent notifications in the web UI's bell, newest first, with how many are unread.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		n := GetDeps().Notifier
		if n == nil {
			return nil, struct{}{}, errNoNotifier
		}
		list, unread := n.History()
		return jsonResult(map[string]any{"notifications": list, "unread": unread})
	})
	addTool(s, &sdkmcp.Tool{
		Name:        "notification_settings_get",
		Description: "Return the user's notification settings: the master switch (enabled), delivery methods (system, inApp, server), sound, quiet hours, and per-event-type settings, plus the event types and sounds available.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		settings, err := notify.ReadSettings()
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(map[string]any{"settings": settings, "eventTypes": notify.EventTypes, "sounds": notify.Sounds})
	})
	addTool(s, &sdkmcp.Tool{
		Name: "notification_settings_set",
		Description: "Replace the user's notification settings. settings is the full object as notification_settings_get returns it under \"settings\"; values Aviary does not offer fall back to their defaults. " +
			"reset=true restores the defaults instead.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args notificationSettingsSetArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		if args.Reset {
			saved, err := notify.WriteSettings(nil)
			if err != nil {
				return nil, struct{}{}, err
			}
			return jsonResult(saved)
		}
		if args.Settings == nil {
			return nil, struct{}{}, errors.New("settings or reset is required")
		}
		saved, err := notify.WriteSettings(args.Settings)
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(saved)
	})
}
