package cmd

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	internalmcp "github.com/lsegal/aviary/internal/mcp"
	"github.com/lsegal/aviary/internal/scheduler"
	"github.com/lsegal/aviary/internal/store"
)

func setupTaskCLI(t *testing.T) *scheduler.Scheduler {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	store.SetDataDir(base + "/aviary")
	t.Cleanup(func() { store.SetDataDir("") })
	require.NoError(t, store.EnsureDirs())

	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}}}
	require.NoError(t, config.Save("", cfg))
	mgr := agent.NewManager(nil)
	mgr.Reconcile(cfg)
	s, err := scheduler.New(mgr, 1)
	require.NoError(t, err)
	t.Cleanup(s.Stop)
	s.Reconcile(cfg)

	oldDeps, oldDispatcher := internalmcp.GetDeps(), dispatcher
	t.Cleanup(func() {
		internalmcp.SetDeps(oldDeps)
		dispatcher = oldDispatcher
		internalmcp.SetServerChecker(func() bool { return false })
	})
	internalmcp.SetServerChecker(func() bool { return false })
	internalmcp.SetDeps(&internalmcp.Deps{Agents: mgr, Scheduler: s})
	dispatcher = internalmcp.NewDispatcher("https://localhost:16677", "")
	return s
}

// runTaskCmd runs c with args and returns its output, resetting flag state
// so commands can run more than once in a test binary.
func runTaskCmd(t *testing.T, c *cobra.Command, args ...string) (string, error) {
	t.Helper()
	c.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	require.NoError(t, c.Flags().Parse(args))
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetContext(t.Context())
	err := c.RunE(c, c.Flags().Args())
	return out.String(), err
}

func TestTaskCLI_AddListPreviewRunRemove(t *testing.T) {
	s := setupTaskCLI(t)

	out, err := runTaskCmd(t, taskAddCmd, "bot", "standup", "Summarize yesterday", "--days", "mon-fri", "--at", "09:00", "--timezone", "UTC", "--overlap", "skip")
	require.NoError(t, err)
	assert.Contains(t, out, "weekly on Mon, Tue, Wed, Thu, Fri at 09:00 (UTC)")

	_, err = runTaskCmd(t, taskAddCmd, "bot", "nothing", "x")
	assert.ErrorContains(t, err, "a trigger is required")

	out, err = runTaskCmd(t, taskListCmd)
	require.NoError(t, err)
	assert.Contains(t, out, "bot/standup")
	assert.Contains(t, out, "weekly on Mon")

	out, err = runTaskCmd(t, taskPreviewCmd, "standup")
	require.NoError(t, err)
	assert.Contains(t, out, "09:00 UTC")
	assert.Len(t, bytes.Split(bytes.TrimSpace([]byte(out)), []byte("\n")), 4)

	out, err = runTaskCmd(t, taskPreviewCmd, "--every", "15m", "--count", "2")
	require.NoError(t, err)
	assert.Contains(t, out, "every 15m0s")

	out, err = runTaskCmd(t, taskUpdateCmd, "standup", "--at", "10:30")
	require.NoError(t, err)
	assert.Contains(t, out, "updated")
	assert.Equal(t, "10:30", s.ListTasks()[0].At)

	out, err = runTaskCmd(t, taskRunCmd, "standup")
	require.NoError(t, err)
	assert.Contains(t, out, `Started task "bot/standup"`)

	out, err = runTaskCmd(t, taskRmCmd, "bot/standup")
	require.NoError(t, err)
	assert.Contains(t, out, "deleted")
	assert.Empty(t, s.ListTasks())
}

func TestExpandDayRange(t *testing.T) {
	assert.Equal(t, "mon,tue,wed,thu,fri", expandDayRange("mon-fri"))
	assert.Equal(t, "fri,sat,sun,mon", expandDayRange("Friday-Monday"))
	assert.Equal(t, "mon,wed", expandDayRange("mon,wed"))
}
