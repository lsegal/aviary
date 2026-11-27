package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/projects"
	"github.com/lsegal/aviary/internal/store"
)

// setupUsageTools points the store at a fresh directory and returns a
// dispatcher that calls the usage tools in process.
func setupUsageTools(t *testing.T) *Dispatcher {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	require.NoError(t, store.EnsureDirs())
	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })
	SetDeps(&Deps{Agents: agent.NewManager(nil)})
	return NewDispatcher("https://localhost:16677", "")
}

func appendUsage(t *testing.T, recs ...domain.UsageRecord) {
	t.Helper()
	for _, r := range recs {
		require.NoError(t, store.AppendJSONL(store.UsagePath(), r))
	}
}

func callUsageSessions(t *testing.T, d *Dispatcher, args map[string]any) UsageSessionsPage {
	t.Helper()
	out, err := d.CallTool(context.Background(), "usage_sessions", args)
	require.NoError(t, err)
	var page UsageSessionsPage
	require.NoError(t, json.Unmarshal([]byte(out), &page), out)
	return page
}

// assertToolRejects checks that calling tool with args fails with want; the
// dispatcher reports a tool's error as its text.
func assertToolRejects(t *testing.T, d *Dispatcher, tool string, args map[string]any, want string) {
	t.Helper()
	out, err := d.CallTool(context.Background(), tool, args)
	if err != nil {
		assert.Contains(t, err.Error(), want)
		return
	}
	assert.Contains(t, out, want)
}

func sessionIDs(rows []UsageSessionRow) []string {
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.SessionID
	}
	return ids
}

func TestUsageSessionsPaginatesAndSorts(t *testing.T) {
	d := setupUsageTools(t)
	base := time.Now().Add(-time.Hour)
	for i := range 120 {
		appendUsage(t, domain.UsageRecord{
			Timestamp: base.Add(time.Duration(i) * time.Second), AgentID: "bot",
			SessionID: fmt.Sprintf("s%03d", i), Model: "m", InputTokens: i, OutputTokens: 1, ToolCalls: i % 7,
			SessionName: fmt.Sprintf("Chat %03d", i), SessionKind: domain.UsageKindChat,
		})
	}

	first := callUsageSessions(t, d, map[string]any{})
	assert.Equal(t, 120, first.Total)
	assert.Equal(t, 50, first.Limit)
	require.Len(t, first.Sessions, 50)
	assert.Equal(t, "s119", first.Sessions[0].SessionID, "newest first by default")

	last := callUsageSessions(t, d, map[string]any{"offset": 100})
	require.Len(t, last.Sessions, 20)
	assert.Equal(t, "s019", last.Sessions[0].SessionID)
	assert.Equal(t, "s000", last.Sessions[19].SessionID)

	byTokens := callUsageSessions(t, d, map[string]any{"sort": "tokens", "order": "asc", "limit": 3})
	assert.Equal(t, []string{"s000", "s001", "s002"}, sessionIDs(byTokens.Sessions))

	byName := callUsageSessions(t, d, map[string]any{"sort": "name", "order": "desc", "limit": 2, "offset": 1})
	assert.Equal(t, []string{"s118", "s117"}, sessionIDs(byName.Sessions))

	// Totals cover every session, not just the page.
	wantInput := 0
	for i := range 120 {
		wantInput += i
	}
	assert.Equal(t, wantInput, byName.Totals.Input)
	assert.Equal(t, 120, byName.Totals.Output)
	assert.Equal(t, 120, byName.Totals.Messages)

	assertToolRejects(t, d, "usage_sessions", map[string]any{"sort": "bogus"}, "unknown sort")
}

func TestUsageSessionsFilters(t *testing.T) {
	d := setupUsageTools(t)
	live, err := agent.NewSessionManager().CreateWithName("alpha", "")
	require.NoError(t, err)
	now := time.Now().Add(-time.Minute)
	appendUsage(t,
		domain.UsageRecord{Timestamp: now, AgentID: "alpha", SessionID: live.ID, Model: "m1", InputTokens: 10, CostUSD: 0.5},
		domain.UsageRecord{Timestamp: now, AgentID: "alpha", SessionID: live.ID, Model: "m2", InputTokens: 5},
		domain.UsageRecord{Timestamp: now, AgentID: "alpha", SessionID: "gone", Model: "m1", InputTokens: 20, HasError: true},
		domain.UsageRecord{Timestamp: now, AgentID: "beta", SessionID: "gone2", Model: "m2", InputTokens: 40},
		domain.UsageRecord{Timestamp: now.AddDate(0, 0, -40), AgentID: "beta", SessionID: "old", Model: "m3", InputTokens: 80},
		domain.UsageRecord{Timestamp: now, AgentID: projects.UsageAgentID, SessionID: "o/r#1", Project: &domain.ProjectUsage{Issue: "o/r#1", DurationMS: 1}},
	)

	all := callUsageSessions(t, d, map[string]any{})
	assert.Equal(t, 3, all.Total)
	assert.Equal(t, []string{"alpha", "beta"}, all.Agents)
	assert.Equal(t, []string{"m1", "m2"}, all.Models)
	assert.Equal(t, 75, all.Totals.Input)
	require.NotNil(t, all.Totals.CostUSD)
	assert.InDelta(t, 0.5, *all.Totals.CostUSD, 1e-9)

	for _, tc := range []struct {
		args  map[string]any
		ids   []string
		input int
	}{
		{map[string]any{"agent": "alpha"}, []string{live.ID, "gone"}, 35},
		{map[string]any{"model": "m1"}, []string{live.ID, "gone"}, 30},
		{map[string]any{"status": "error"}, []string{"gone"}, 20},
		{map[string]any{"status": "ok"}, []string{live.ID, "gone2"}, 55},
		{map[string]any{"deleted": "exclude"}, []string{live.ID}, 15},
		{map[string]any{"deleted": "only"}, []string{"gone", "gone2"}, 60},
		{map[string]any{"start": now.AddDate(0, 0, -45).Format("2006-01-02"), "agent": "beta"}, []string{"gone2", "old"}, 120},
	} {
		page := callUsageSessions(t, d, tc.args)
		assert.ElementsMatch(t, tc.ids, sessionIDs(page.Sessions), "%v", tc.args)
		assert.Equal(t, tc.input, page.Totals.Input, "%v", tc.args)
		assert.Equal(t, len(tc.ids), page.Total, "%v", tc.args)

		// usage_query applies the same filters, so the page's totals and
		// charts match the sessions list.
		out, err := d.CallTool(context.Background(), "usage_query", tc.args)
		require.NoError(t, err)
		var recs []domain.UsageRecord
		require.NoError(t, json.Unmarshal([]byte(out), &recs))
		input, projectRecords := 0, 0
		for _, r := range recs {
			if r.Project != nil {
				projectRecords++
			}
			input += r.InputTokens
		}
		assert.Equal(t, tc.input, input, "usage_query %v", tc.args)
		assert.Equal(t, 1, projectRecords, "Code Loop work stays in every query")
	}

	assertToolRejects(t, d, "usage_sessions", map[string]any{"status": "bogus"}, "unknown status")
	assertToolRejects(t, d, "usage_query", map[string]any{"deleted": "bogus"}, "unknown deleted")
}

func TestUsageSessionsKeepNamesOfDeletedSessions(t *testing.T) {
	d := setupUsageTools(t)
	m := agent.NewSessionManager()
	now := time.Now().Add(-time.Minute)

	// A chat recorded before snapshots existed keeps its name through its
	// tombstone once deleted.
	legacy, err := m.CreateWithName("bot", "")
	require.NoError(t, err)
	require.NoError(t, m.Rename("bot", legacy.ID, "Trip planning"))
	// A chat whose records snapshot its name.
	snapped, err := m.CreateWithName("bot", "")
	require.NoError(t, err)
	// A chat still alive, renamed since its record was written.
	alive, err := m.CreateWithName("bot", "")
	require.NoError(t, err)
	require.NoError(t, m.Rename("bot", alive.ID, "Renamed chat"))

	appendUsage(t,
		domain.UsageRecord{Timestamp: now, AgentID: "bot", SessionID: legacy.ID, InputTokens: 1},
		domain.UsageRecord{Timestamp: now, AgentID: "bot", SessionID: snapped.ID, InputTokens: 1, SessionName: "New chat", SessionKind: domain.UsageKindChat},
		domain.UsageRecord{Timestamp: now.Add(time.Second), AgentID: "bot", SessionID: snapped.ID, InputTokens: 1, SessionName: "Budget review", SessionKind: domain.UsageKindChat},
		domain.UsageRecord{Timestamp: now, AgentID: "bot", SessionID: alive.ID, InputTokens: 1, SessionName: "Old name", SessionKind: domain.UsageKindChat},
		domain.UsageRecord{Timestamp: now, AgentID: "bot", SessionID: "never-named", InputTokens: 1},
		domain.UsageRecord{Timestamp: now, AgentID: "bot", SessionID: "digest", Task: "bot/digest", InputTokens: 1},
		domain.UsageRecord{Timestamp: now, AgentID: "Aviary", SessionID: "sess_job", InputTokens: 1, SessionName: "Fix the parser", SessionKind: domain.UsageKindJob, SessionOwner: "o/r#7"},
		domain.UsageRecord{Timestamp: now, AgentID: projects.UsageAgentID, SessionID: "o/r#8", InputTokens: 1},
	)
	require.NoError(t, m.Delete("bot", legacy.ID))
	require.NoError(t, m.Delete("bot", snapped.ID))

	page := callUsageSessions(t, d, map[string]any{"limit": 100})
	got := map[string]UsageSessionRow{}
	for _, r := range page.Sessions {
		got[r.SessionID] = r
	}

	assert.Equal(t, "Trip planning", got[legacy.ID].Name)
	assert.True(t, got[legacy.ID].Deleted)
	require.NotNil(t, got[legacy.ID].DeletedAt)

	assert.Equal(t, "Budget review", got[snapped.ID].Name, "the latest snapshot names the session")
	assert.True(t, got[snapped.ID].Deleted)
	assert.Equal(t, domain.UsageKindChat, got[snapped.ID].Kind)

	assert.Equal(t, "Renamed chat", got[alive.ID].Name)
	assert.False(t, got[alive.ID].Deleted)

	assert.Equal(t, UnknownSessionName, got["never-named"].Name)
	assert.True(t, got["never-named"].Deleted)
	assert.Nil(t, got["never-named"].DeletedAt)

	assert.Equal(t, "digest", got["digest"].Name)
	assert.Equal(t, domain.UsageKindTask, got["digest"].Kind)

	assert.Equal(t, "Fix the parser", got["sess_job"].Name)
	assert.Equal(t, domain.UsageKindJob, got["sess_job"].Kind)
	assert.Equal(t, "o/r#7", got["sess_job"].Owner)
	assert.True(t, got["sess_job"].Deleted)

	assert.Equal(t, "o/r#8", got["o/r#8"].Name, "Code Loop job runs are named after their issue")
	assert.Equal(t, domain.UsageKindJob, got["o/r#8"].Kind)
	assert.False(t, got["o/r#8"].Deleted, "Code Loop job runs have no session to delete")
}
