package notify

import (
	"slices"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/store"
)

// The event types a notification can have. Each has its own settings.
const (
	TypeChatReply       = "chat.reply"
	TypeCodeLoopFailed  = "codeloop.failed"
	TypeCodeLoopBlocked = "codeloop.blocked"
	TypeCodeLoopDone    = "codeloop.done"
	TypeTaskRun         = "task.run"
	TypeWorkflowFailed  = "workflow.failed"
	TypeCustom          = "custom"
	// TypeTest is the settings page's Test notification. It has no settings
	// of its own and reaches every enabled delivery method.
	TypeTest = "test"
)

// The sections of Aviary events come from.
const (
	SectionChats     = "chats"
	SectionCodeLoop  = "codeloop"
	SectionTasks     = "tasks"
	SectionWorkflows = "workflows"
	SectionSystem    = "system"
)

// The delivery styles an event type can have.
const (
	DeliveryBig    = "big"
	DeliverySmall  = "small"
	DeliverySystem = "system"
)

// SoundNone silences one event type.
const SoundNone = "none"

// Sounds are the sound types the web UI bundles.
var Sounds = []string{"chime", "bell", "ping", "pop", "marimba", "alert"}

// EventType describes one configurable event type.
type EventType struct {
	Type    string `json:"type"`
	Section string `json:"section"`
	Label   string `json:"label"`
	// Default is the type's setting for a new user.
	Default store.NotificationEvent `json:"default"`
}

// EventTypes lists every configurable event type, in settings order.
var EventTypes = []EventType{
	{TypeChatReply, SectionChats, "Agent replied in a chat you're not viewing", store.NotificationEvent{Enabled: true, Delivery: DeliverySmall}},
	{TypeCodeLoopFailed, SectionCodeLoop, "Code Loop job failed", store.NotificationEvent{Enabled: true, Delivery: DeliveryBig}},
	{TypeCodeLoopBlocked, SectionCodeLoop, "Code Loop job blocked or waiting on you", store.NotificationEvent{Enabled: true, Delivery: DeliveryBig}},
	{TypeCodeLoopDone, SectionCodeLoop, "Code Loop job done or pull request merged", store.NotificationEvent{Enabled: false, Delivery: DeliverySmall}},
	{TypeTaskRun, SectionTasks, "Task or workflow with the Notify target finished", store.NotificationEvent{Enabled: true, Delivery: DeliverySmall}},
	{TypeWorkflowFailed, SectionWorkflows, "Workflow failed", store.NotificationEvent{Enabled: true, Delivery: DeliverySmall}},
	{TypeCustom, SectionSystem, "Sent by an agent or the notify API", store.NotificationEvent{Enabled: true, Delivery: DeliverySmall}},
}

func eventType(typ string) (EventType, bool) {
	for _, t := range EventTypes {
		if t.Type == typ {
			return t, true
		}
	}
	return EventType{}, false
}

// DefaultSettings are a new user's notification settings: on, with in-app
// and system delivery, and sound off.
func DefaultSettings() store.NotificationPreferences {
	prefs := store.NotificationPreferences{
		Enabled:    true,
		System:     true,
		InApp:      true,
		Sound:      store.NotificationSound{Enabled: false, Type: Sounds[0], Volume: 60},
		QuietHours: store.NotificationQuietHours{Start: "22:00", End: "07:00"},
		Events:     map[string]store.NotificationEvent{},
	}
	for _, t := range EventTypes {
		prefs.Events[t.Type] = t.Default
	}
	return prefs
}

// Normalize fills what prefs leaves out with the defaults and drops values
// the web UI does not offer. Nil prefs are the defaults.
func Normalize(prefs *store.NotificationPreferences) store.NotificationPreferences {
	defaults := DefaultSettings()
	if prefs == nil {
		return defaults
	}
	out := *prefs
	if !slices.Contains(Sounds, out.Sound.Type) {
		out.Sound.Type = defaults.Sound.Type
	}
	out.Sound.Volume = min(max(out.Sound.Volume, 0), 100)
	if _, ok := parseClock(out.QuietHours.Start); !ok {
		out.QuietHours.Start = defaults.QuietHours.Start
	}
	if _, ok := parseClock(out.QuietHours.End); !ok {
		out.QuietHours.End = defaults.QuietHours.End
	}
	out.Events = map[string]store.NotificationEvent{}
	for _, t := range EventTypes {
		ev, ok := prefs.Events[t.Type]
		if !ok {
			ev = t.Default
		}
		if ev.Delivery != DeliveryBig && ev.Delivery != DeliverySmall && ev.Delivery != DeliverySystem {
			ev.Delivery = t.Default.Delivery
		}
		if ev.Sound != "" && ev.Sound != SoundNone && !slices.Contains(Sounds, ev.Sound) {
			ev.Sound = ""
		}
		out.Events[t.Type] = ev
	}
	return out
}

// ReadSettings loads the user's notification settings.
func ReadSettings() (store.NotificationPreferences, error) {
	prefs, err := store.ReadPreferences()
	if err != nil {
		return DefaultSettings(), err
	}
	return Normalize(prefs.Notifications), nil
}

// WriteSettings saves the user's notification settings, normalized; nil
// restores the defaults.
func WriteSettings(next *store.NotificationPreferences) (store.NotificationPreferences, error) {
	var saved store.NotificationPreferences
	_, err := store.UpdatePreferences(func(p *store.Preferences) {
		if next == nil {
			p.Notifications = nil
			saved = DefaultSettings()
			return
		}
		saved = Normalize(next)
		p.Notifications = &saved
	})
	return saved, err
}

// parseClock reads "HH:MM" as minutes after midnight.
func parseClock(s string) (int, bool) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

// inQuietHours reports whether now falls in q's window, which may span
// midnight.
func inQuietHours(q store.NotificationQuietHours, now time.Time) bool {
	if !q.Enabled {
		return false
	}
	start, ok1 := parseClock(q.Start)
	end, ok2 := parseClock(q.End)
	if !ok1 || !ok2 || start == end {
		return false
	}
	minute := now.Hour()*60 + now.Minute()
	if start < end {
		return minute >= start && minute < end
	}
	return minute >= start || minute < end
}
