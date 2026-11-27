package mcp

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/projects"
	"github.com/lsegal/aviary/internal/store"
)

// UnknownSessionName names a deleted session whose name was never recorded
// and cannot be recovered.
const UnknownSessionName = "Unknown session (deleted)"

// usageFilter narrows the usage records and sessions a query covers. Agent
// and Model match records; Status ("ok" or "error") and Deleted ("include",
// "exclude" or "only") match whole sessions.
type usageFilter struct {
	Start, End     time.Time
	Agent, Model   string
	Status         string
	Deleted        string
	resolveSession sessionResolver
}

// sessionResolver reports whether agentID's session sessionID still exists
// and, when it does, its live display name, which is read only when name
// asks for it.
type sessionResolver struct {
	exists     func(agentID, sessionID string) bool
	title      func(agentID, sessionID string) string
	liveName   func(agentID, sessionID string) string
	tombstones map[string]domain.DeletedSession
}

// storeSessionResolver resolves sessions from the session store and the
// deleted-session tombstones.
func storeSessionResolver() sessionResolver {
	tombstones := map[string]domain.DeletedSession{}
	if list, err := store.ReadJSONL[domain.DeletedSession](store.DeletedSessionsPath()); err == nil {
		for _, t := range list {
			tombstones[usageSessionKey(t.AgentID, t.SessionID)] = t
		}
	}
	return sessionResolver{
		exists:     func(agentID, sessionID string) bool { return store.FindSessionPath(agentID, sessionID) != "" },
		title:      agent.SessionTitle,
		liveName:   agent.LiveSessionName,
		tombstones: tombstones,
	}
}

// UsageSessionRow is one session's usage over the queried range.
type UsageSessionRow struct {
	SessionID   string   `json:"session_id"`
	AgentID     string   `json:"agent_id"`
	Model       string   `json:"model"`
	Provider    string   `json:"provider"`
	Input       int      `json:"input"`
	Output      int      `json:"output"`
	CacheRead   int      `json:"cache_read"`
	CacheWrite  int      `json:"cache_write"`
	ToolCalls   int      `json:"tool_calls"`
	Messages    int      `json:"messages"`
	CostUSD     *float64 `json:"cost_usd,omitempty"`
	HasError    bool     `json:"has_error"`
	HasThrottle bool     `json:"has_throttle"`
	FirstTS     string   `json:"first_ts"`
	LastTS      string   `json:"last_ts"`
	// Name is the session's live display name, else the name its records
	// snapshotted, else one recovered from its task, job or tombstone, else
	// UnknownSessionName.
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Owner   string `json:"owner,omitempty"`
	Task    string `json:"task,omitempty"`
	Channel string `json:"channel,omitempty"`
	// Deleted marks a session that no longer exists; DeletedAt is when, if
	// its deletion was recorded.
	Deleted   bool       `json:"deleted,omitempty"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`

	first, last time.Time
}

// UsageTotals sums the usage of every session a query matches.
type UsageTotals struct {
	Input      int      `json:"input"`
	Output     int      `json:"output"`
	CacheRead  int      `json:"cache_read"`
	CacheWrite int      `json:"cache_write"`
	ToolCalls  int      `json:"tool_calls"`
	Messages   int      `json:"messages"`
	Errors     int      `json:"errors"`
	CostUSD    *float64 `json:"cost_usd,omitempty"`
}

// UsageSessionsPage is one page of the sessions a query matches, sorted,
// with the count and totals over all of them and the agents and models in
// range to filter by.
type UsageSessionsPage struct {
	Sessions []UsageSessionRow `json:"sessions"`
	Total    int               `json:"total"`
	Offset   int               `json:"offset"`
	Limit    int               `json:"limit"`
	Totals   UsageTotals       `json:"totals"`
	Agents   []string          `json:"agents"`
	Models   []string          `json:"models"`
}

func usageSessionKey(agentID, sessionID string) string { return agentID + "\x00" + sessionID }

// isTokenRecord reports whether r records token use rather than Code Loop
// work or a task's delivery.
func isTokenRecord(r domain.UsageRecord) bool { return r.Project == nil && r.Delivery == nil }

// parseUsageRange is the [start, end) range the start and end arguments
// name, RFC 3339 or YYYY-MM-DD, defaulting to the last 30 days. A bare end
// date includes that whole day.
func parseUsageRange(startArg, endArg string) (time.Time, time.Time) {
	now := time.Now()
	parse := func(s string, fallback time.Time) time.Time {
		if s == "" {
			return fallback
		}
		for _, layout := range []string{time.RFC3339, "2006-01-02"} {
			if t, err := time.Parse(layout, s); err == nil {
				return t
			}
		}
		return fallback
	}
	start := parse(startArg, now.AddDate(0, 0, -30))
	end := parse(endArg, now)
	if len(endArg) == 10 {
		end = end.AddDate(0, 0, 1)
	}
	return start, end
}

// readUsageRange reads the usage records in [start, end).
func readUsageRange(start, end time.Time) ([]domain.UsageRecord, error) {
	records, err := store.ReadJSONL[domain.UsageRecord](store.UsagePath())
	if err != nil {
		return nil, fmt.Errorf("reading usage log: %w", err)
	}
	filtered := records[:0]
	for _, r := range records {
		if !r.Timestamp.Before(start) && r.Timestamp.Before(end) {
			filtered = append(filtered, r)
		}
	}
	return filtered, nil
}

// matchesRecord reports whether token record r passes the agent and model
// filters.
func (f usageFilter) matchesRecord(r domain.UsageRecord) bool {
	return (f.Agent == "" || r.AgentID == f.Agent) && (f.Model == "" || usageModelName(r) == f.Model)
}

// usageModelName is the provider/model name of r's model. Older Code Loop
// records store the bare model ("opus") beside provider "claude", while
// core's prompts store "claude/opus"; both name the same model.
func usageModelName(r domain.UsageRecord) string {
	model, provider := strings.TrimSpace(r.Model), strings.TrimSpace(r.Provider)
	if model == "" || provider == "" || model == provider || strings.HasPrefix(model, provider+"/") {
		return model
	}
	return provider + "/" + model
}

// matchesSession reports whether row passes the status and deleted filters.
func (f usageFilter) matchesSession(row UsageSessionRow) bool {
	switch f.Status {
	case "ok":
		if row.HasError {
			return false
		}
	case "error":
		if !row.HasError {
			return false
		}
	}
	switch f.Deleted {
	case "exclude":
		return !row.Deleted
	case "only":
		return row.Deleted
	}
	return true
}

// usageSessions aggregates the token records that pass f's record filters
// into sessions, names them, and keeps those that pass its session filters,
// newest first.
func usageSessions(records []domain.UsageRecord, f usageFilter) []UsageSessionRow {
	rows := map[string]*UsageSessionRow{}
	var order []string
	snapshots := map[string]domain.UsageRecord{}
	for _, r := range records {
		if !isTokenRecord(r) || !f.matchesRecord(r) {
			continue
		}
		key := usageSessionKey(r.AgentID, r.SessionID)
		row := rows[key]
		if row == nil {
			row = &UsageSessionRow{
				SessionID: r.SessionID, AgentID: r.AgentID, Model: usageModelName(r), Provider: r.Provider,
				Task: r.Task, Channel: r.Channel, first: r.Timestamp, last: r.Timestamp,
			}
			rows[key] = row
			order = append(order, key)
		}
		row.Input += r.InputTokens
		row.Output += r.OutputTokens
		row.CacheRead += r.CacheReadTokens
		row.CacheWrite += r.CacheWriteTokens
		row.ToolCalls += r.ToolCalls
		row.Messages++
		if r.CostUSD != 0 {
			row.CostUSD = addCost(row.CostUSD, r.CostUSD)
		}
		row.HasError = row.HasError || r.HasError
		row.HasThrottle = row.HasThrottle || r.HasThrottle
		if r.Timestamp.Before(row.first) {
			row.first = r.Timestamp
		}
		if !r.Timestamp.Before(row.last) {
			row.last = r.Timestamp
		}
		if r.SessionName != "" || r.SessionKind != "" {
			if prev, ok := snapshots[key]; !ok || !r.Timestamp.Before(prev.Timestamp) {
				snapshots[key] = r
			}
		}
	}
	out := make([]UsageSessionRow, 0, len(order))
	for _, key := range order {
		row := rows[key]
		f.resolveSession.name(row, snapshots[key])
		row.FirstTS = row.first.Format(time.RFC3339Nano)
		row.LastTS = row.last.Format(time.RFC3339Nano)
		if f.matchesSession(*row) {
			out = append(out, *row)
		}
	}
	slices.SortStableFunc(out, func(a, b UsageSessionRow) int { return b.last.Compare(a.last) })
	return out
}

// name fills row's name, kind, owner and deleted state from the session's
// live state, its latest snapshot, its task or job, and its tombstone.
func (s sessionResolver) name(row *UsageSessionRow, snap domain.UsageRecord) {
	row.Kind, row.Owner = snap.SessionKind, snap.SessionOwner
	codeLoop := row.AgentID == projects.UsageAgentID
	if row.Kind == "" {
		switch {
		case codeLoop:
			row.Kind, row.Owner = domain.UsageKindJob, row.SessionID
		case row.Task != "" && row.Channel != "":
			row.Kind, row.Owner = domain.UsageKindWorkflow, row.Channel
		case row.Task != "":
			row.Kind, row.Owner = domain.UsageKindTask, row.Task
		default:
			row.Kind = domain.UsageKindChat
		}
	}
	// Code Loop's own job runs have no session to delete: their session
	// ID is the issue they worked on.
	exists := codeLoop || s.exists(row.AgentID, row.SessionID)
	if !exists {
		row.Deleted = true
		if t, ok := s.tombstones[usageSessionKey(row.AgentID, row.SessionID)]; ok {
			row.DeletedAt = &t.DeletedAt
			if snap.SessionName == "" {
				snap.SessionName = t.Name
			}
		}
	}
	var live string
	if exists && !codeLoop && row.Kind == domain.UsageKindChat {
		live = s.title(row.AgentID, row.SessionID)
	}
	row.Name = cmp.Or(live, snap.SessionName)
	if row.Name == "" {
		switch {
		case row.Task != "":
			row.Name = row.Task[strings.Index(row.Task, "/")+1:]
		case codeLoop:
			row.Name = row.SessionID
		case exists:
			row.Name = s.liveName(row.AgentID, row.SessionID)
		}
	}
	if row.Name == "" {
		row.Name = UnknownSessionName
	}
}

// addCost adds v to the cost total, which is nil until a cost is reported.
func addCost(total *float64, v float64) *float64 {
	sum := v
	if total != nil {
		sum += *total
	}
	return &sum
}

// totalUsage sums rows.
func totalUsage(rows []UsageSessionRow) UsageTotals {
	var t UsageTotals
	for _, r := range rows {
		t.Input += r.Input
		t.Output += r.Output
		t.CacheRead += r.CacheRead
		t.CacheWrite += r.CacheWrite
		t.ToolCalls += r.ToolCalls
		t.Messages += r.Messages
		if r.HasError {
			t.Errors++
		}
		if r.CostUSD != nil {
			t.CostUSD = addCost(t.CostUSD, *r.CostUSD)
		}
	}
	return t
}

// The columns the sessions list sorts by.
var usageSessionSorts = map[string]func(a, b UsageSessionRow) int{
	"name": func(a, b UsageSessionRow) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	},
	"agent":  func(a, b UsageSessionRow) int { return strings.Compare(a.AgentID, b.AgentID) },
	"model":  func(a, b UsageSessionRow) int { return strings.Compare(a.Model, b.Model) },
	"tokens": func(a, b UsageSessionRow) int { return cmp.Compare(a.Input+a.Output, b.Input+b.Output) },
	"tools":  func(a, b UsageSessionRow) int { return cmp.Compare(a.ToolCalls, b.ToolCalls) },
	"last":   func(a, b UsageSessionRow) int { return a.last.Compare(b.last) },
}

// sortUsageSessions sorts rows by column, "last" when empty, in order "asc"
// or "desc". Text columns default to ascending and the rest to descending;
// ties fall back to newest first.
func sortUsageSessions(rows []UsageSessionRow, column, order string) error {
	column = cmp.Or(column, "last")
	by, ok := usageSessionSorts[column]
	if !ok {
		return fmt.Errorf("unknown sort %q: use name, agent, model, tokens, tools or last", column)
	}
	if order == "" {
		order = "desc"
		if column == "name" || column == "agent" || column == "model" {
			order = "asc"
		}
	}
	if order != "asc" && order != "desc" {
		return fmt.Errorf("unknown order %q: use asc or desc", order)
	}
	slices.SortStableFunc(rows, func(a, b UsageSessionRow) int {
		c := by(a, b)
		if order == "desc" {
			c = -c
		}
		return cmp.Or(c, b.last.Compare(a.last), strings.Compare(a.SessionID, b.SessionID))
	})
	return nil
}

// The page sizes the sessions list allows.
const (
	defaultUsageSessionsLimit = 50
	maxUsageSessionsLimit     = 500
)

// pageUsageSessions builds the page of rows at offset, limit long, from
// every matching row and the records in range.
func pageUsageSessions(records []domain.UsageRecord, rows []UsageSessionRow, offset, limit int) UsageSessionsPage {
	if limit <= 0 {
		limit = defaultUsageSessionsLimit
	}
	limit = min(limit, maxUsageSessionsLimit)
	offset = max(0, min(offset, len(rows)))
	agents, models := map[string]bool{}, map[string]bool{}
	for _, r := range records {
		if !isTokenRecord(r) {
			continue
		}
		agents[r.AgentID] = true
		if m := usageModelName(r); m != "" {
			models[m] = true
		}
	}
	page := rows[offset:min(offset+limit, len(rows))]
	return UsageSessionsPage{
		Sessions: append(make([]UsageSessionRow, 0, len(page)), page...),
		Total:    len(rows),
		Offset:   offset,
		Limit:    limit,
		Totals:   totalUsage(rows),
		Agents:   sortedKeys(agents),
		Models:   sortedKeys(models),
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// filterUsageRecords keeps the records the sessions in rows spent, plus
// every record of Code Loop work and delivery, which belong to no session's
// tokens.
func filterUsageRecords(records []domain.UsageRecord, f usageFilter, rows []UsageSessionRow) []domain.UsageRecord {
	keep := make(map[string]bool, len(rows))
	for _, r := range rows {
		keep[usageSessionKey(r.AgentID, r.SessionID)] = true
	}
	out := make([]domain.UsageRecord, 0, len(records))
	for _, r := range records {
		if !isTokenRecord(r) || (f.matchesRecord(r) && keep[usageSessionKey(r.AgentID, r.SessionID)]) {
			out = append(out, r)
		}
	}
	return out
}
