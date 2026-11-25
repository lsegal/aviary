package scriptruntime

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
)

type fakeToolCall struct {
	name string
	args map[string]any
}

type fakeToolClient struct {
	tools   []agent.ToolInfo
	listErr error
	results map[string]string
	errs    map[string]error
	calls   []fakeToolCall
}

func (f *fakeToolClient) ListTools(context.Context) ([]agent.ToolInfo, error) {
	return f.tools, f.listErr
}

func (f *fakeToolClient) CallToolText(_ context.Context, name string, args map[string]any) (string, error) {
	f.calls = append(f.calls, fakeToolCall{name: name, args: args})
	if err := f.errs[name]; err != nil {
		return "", err
	}
	return f.results[name], nil
}

func (f *fakeToolClient) Close() error { return nil }

func newFakeTools(names ...string) *fakeToolClient {
	client := &fakeToolClient{results: map[string]string{}, errs: map[string]error{}}
	for _, name := range names {
		client.tools = append(client.tools, agent.ToolInfo{Name: name})
	}
	return client
}

func TestRunLua_EmptyScript(t *testing.T) {
	_, err := RunLua(context.Background(), "  \n\t", Options{})
	require.EqualError(t, err, "script is empty")
	require.EqualError(t, ValidateLua(" "), "script is empty")
}

func TestRunLua_PrintJoinsArgumentsAndLogs(t *testing.T) {
	var logs []string
	got, err := RunLua(context.Background(), `print("a", 1, true) print("b")`, Options{
		Logf: func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) },
	})
	require.NoError(t, err)
	assert.Equal(t, "a\t1\ttrue\nb", got)
	assert.Equal(t, []string{"print: a\t1\ttrue", "print: b"}, logs)
}

func TestRunLua_EnvironmentAndTrigger(t *testing.T) {
	got, err := RunLua(context.Background(), `
		print(environment.agent_id, environment.session_id, environment.task_id, environment.job_id)
		print(trigger.text, trigger.meta.count, trigger.tags[2], trigger.ok)
	`, Options{Environment: Environment{
		AgentID:   "agent-1",
		SessionID: "sess-2",
		TaskID:    "task-3",
		JobID:     "job-4",
		Trigger: map[string]any{
			"text": "hello",
			"meta": map[string]any{"count": 3},
			"tags": []any{"x", "y"},
			"ok":   true,
		},
	}})
	require.NoError(t, err)
	assert.Equal(t, "agent-1\tsess-2\ttask-3\tjob-4\nhello\t3\ty\ttrue", got)
}

func TestRunLua_TriggerUnsetOutsideWorkflows(t *testing.T) {
	got, err := RunLua(context.Background(), `print(trigger == nil)`, Options{})
	require.NoError(t, err)
	assert.Equal(t, "true", got)
}

func TestRunLua_SandboxRemovesFileLoaders(t *testing.T) {
	got, err := RunLua(context.Background(), `print(dofile, loadfile, io, os.execute)`, Options{})
	require.NoError(t, err)
	assert.Equal(t, "nil\tnil\tnil\tnil", got)
}

func TestRunLua_ToolCallsPassArgumentsAndDecodeResults(t *testing.T) {
	client := newFakeTools("lookup", "raw", "noargs")
	client.results["lookup"] = `{"name":"alice","items":[1,2]}`
	client.results["raw"] = "plain text"
	client.results["noargs"] = "done"
	var logs []string

	got, err := RunLua(context.Background(), `
		local r = tool.lookup({id = "u1", flags = {"a", "b"}, nested = {n = 2}, empty = {}})
		print(r.name, r.items[2])
		print(tool.raw({}))
		print(tool.noargs())
	`, Options{
		ToolClient: client,
		Logf:       func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) },
	})
	require.NoError(t, err)
	assert.Equal(t, "alice\t2\nplain text\ndone", got)

	require.Len(t, client.calls, 3)
	assert.Equal(t, "lookup", client.calls[0].name)
	assert.Equal(t, map[string]any{
		"id":     "u1",
		"flags":  []any{"a", "b"},
		"nested": map[string]any{"n": float64(2)},
		"empty":  map[string]any{},
	}, client.calls[0].args)
	assert.Equal(t, map[string]any{}, client.calls[1].args)
	assert.Equal(t, map[string]any{}, client.calls[2].args)

	joined := strings.Join(logs, "\n")
	// Tool results that are JSON are pretty-printed in the log.
	assert.Contains(t, joined, "tool lookup result: {\n  \"items\": [\n    1,\n    2\n  ],\n  \"name\": \"alice\"\n}")
	assert.Contains(t, joined, "tool raw result: plain text")
	assert.Contains(t, joined, "tool raw args: {}")
}

func TestRunLua_ToolErrors(t *testing.T) {
	tests := []struct {
		name    string
		script  string
		wantErr string
		wantLog string
	}{
		{"tool failure", `tool.fail({})`, "backend down", "tool fail error: backend down"},
		{"non-table argument", `tool.fail("x")`, "at most one table argument", "tool fail argument error"},
		{"too many arguments", `tool.fail({}, {})`, "at most one table argument", "tool fail argument error"},
		{"array argument", `tool.fail({1, 2})`, "object-like table argument", "tool fail argument error"},
		{"non-string keys", `tool.fail({[true] = 1})`, "require string keys", "tool fail argument error"},
		{"unsupported nested value", `tool.fail({f = function() end})`, "unsupported lua value type function", "tool fail argument error"},
		{"unsupported array item", `tool.fail({list = {1, function() end}})`, "unsupported lua value type function", "tool fail argument error"},
		{"disabled tool", `tool.missing({})`, `tool "missing" is not enabled for this agent`, "error:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newFakeTools("fail")
			client.errs["fail"] = errors.New("backend down")
			var logs []string
			_, err := RunLua(context.Background(), tt.script, Options{
				ToolClient: client,
				Logf:       func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) },
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Contains(t, strings.Join(logs, "\n"), tt.wantLog)
		})
	}
}

func TestRunLua_ToolListErrorAborts(t *testing.T) {
	client := newFakeTools()
	client.listErr = errors.New("list failed")
	_, err := RunLua(context.Background(), `print("never")`, Options{ToolClient: client})
	require.EqualError(t, err, "list failed")
}

func TestRunLua_ToolCallsWithoutLogger(t *testing.T) {
	client := newFakeTools("echo", "fail")
	client.results["echo"] = `"ok"`
	client.errs["fail"] = errors.New("nope")

	got, err := RunLua(context.Background(), `print(tool.echo())`, Options{ToolClient: client})
	require.NoError(t, err)
	assert.Equal(t, "ok", got)

	_, err = RunLua(context.Background(), `tool.fail()`, Options{ToolClient: client})
	require.ErrorContains(t, err, "nope")

	_, err = RunLua(context.Background(), `tool.echo(1)`, Options{ToolClient: client})
	require.ErrorContains(t, err, "at most one table argument")
}

func TestRunLua_JSONEncodeRejectsUnsupportedValues(t *testing.T) {
	_, err := RunLua(context.Background(), `json.encode(function() end)`, Options{})
	require.ErrorContains(t, err, "unsupported lua value type function")

	_, err = RunLua(context.Background(), `json.encode(0/0)`, Options{})
	require.ErrorContains(t, err, "unsupported value: NaN")
}

func TestRunLua_JSONEncodeRejectsMixedTables(t *testing.T) {
	// Tables that are not a plain 1..n sequence encode as objects, which
	// need string keys.
	for _, script := range []string{
		`json.encode({"a", k = "v"})`,
		`json.encode({[0] = "z", "a"})`,
		`json.encode({[1.5] = "x", "a"})`,
	} {
		_, err := RunLua(context.Background(), script, Options{})
		require.ErrorContains(t, err, "require string keys", script)
	}
}

func TestRunLua_OSDateFormats(t *testing.T) {
	// 2024-03-05 14:07:09 UTC, a Tuesday, day 65 of the year.
	const ts = 1709647629
	tests := []struct {
		format string
		want   string
	}{
		{"!%Y %y %m %d", "2024 24 03 05"},
		{"!%H %I %M %S %p", "14 02 07 09 PM"},
		{"!%a %A %b %B", "Tue Tuesday Mar March"},
		{"!%j %w %Z %%", "065 2 UTC %"},
		{"!%c", "Tue Mar  5 14:07:09 2024"},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			got, err := RunLua(context.Background(), fmt.Sprintf("print(os.date(%q, %d))", tt.format, ts), Options{})
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRunLua_OSDateDefaultsAndLocalTime(t *testing.T) {
	before := time.Now()
	got, err := RunLua(context.Background(), `print(os.date())`, Options{})
	require.NoError(t, err)
	parsed, err := time.ParseInLocation(time.ANSIC, got, time.Local)
	require.NoError(t, err)
	assert.WithinDuration(t, before, parsed, 5*time.Second)

	got, err = RunLua(context.Background(), `print(os.date("%Y", 0))`, Options{})
	require.NoError(t, err)
	assert.Equal(t, time.Unix(0, 0).In(time.Local).Format("2006"), got)
}

func TestRunLua_OSDateRejectsBadFormats(t *testing.T) {
	_, err := RunLua(context.Background(), `os.date("%Q")`, Options{})
	require.ErrorContains(t, err, "unsupported os.date format %Q")

	_, err = RunLua(context.Background(), `os.date("abc%")`, Options{})
	require.ErrorContains(t, err, `invalid os.date format "abc%"`)
}

func TestGoToLuaValueConvertsNumericTypes(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	for _, value := range []any{float32(2), 2, int64(2), int32(2), uint(2), uint64(2), uint32(2)} {
		assert.Equal(t, lua.LNumber(2), goToLuaValue(L, value), "%T", value)
	}
	assert.Equal(t, lua.LString("{1 2}"), goToLuaValue(L, struct{ A, B int }{1, 2}))
}

func TestPrettyLogText(t *testing.T) {
	assert.Equal(t, "", prettyLogText("  "))
	assert.Equal(t, "not json", prettyLogText(" not json "))
	assert.Equal(t, "{\n  \"a\": 1\n}", prettyLogText(`{"a":1}`))
}

func TestMustJSONFallsBackToFmt(t *testing.T) {
	assert.Equal(t, `{"a":1}`, mustJSON(map[string]any{"a": 1}))
	assert.Equal(t, "+Inf", mustJSON(math.Inf(1)))
}
