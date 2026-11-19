package cmd

import (
	"context"
	"reflect"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	internalmcp "github.com/lsegal/aviary/internal/mcp"
)

func TestProjectToolArgsFillsThePositionalField(t *testing.T) {
	for _, test := range []struct {
		positional string
		args, want []string
	}{
		{"number", []string{"7", "--target", "o/r"}, []string{"--number", "7", "--target", "o/r"}},
		{"number", []string{"--number", "7"}, []string{"--number", "7"}},
		{"", []string{"--status", "active"}, []string{"--status", "active"}},
		{"target", nil, nil},
	} {
		if got := projectToolArgs(test.positional, test.args); !reflect.DeepEqual(got, test.want) {
			t.Errorf("projectToolArgs(%q, %v) = %v, want %v", test.positional, test.args, got, test.want)
		}
	}
}

// TestEveryProjectToolHasACommand checks the aviary project tree mirrors every
// project_* MCP tool.
func TestEveryProjectToolHasACommand(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test"}, nil)
	internalmcp.Register(server)
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "client"}, nil)
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close() //nolint:errcheck
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	commands := map[string]bool{}
	for _, spec := range projectToolCommands {
		commands[spec.tool] = true
	}
	tools := map[string]bool{}
	for _, tool := range listed.Tools {
		if strings.HasPrefix(tool.Name, "project_") {
			tools[tool.Name] = true
			if !commands[tool.Name] {
				t.Errorf("tool %s has no aviary project command", tool.Name)
			}
		}
	}
	for tool := range commands {
		if !tools[tool] {
			t.Errorf("aviary project calls %s, which is not a registered tool", tool)
		}
	}
}

func TestProjectCommandTree(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"project", "job", "get"})
	if err != nil || cmd.Name() != "get" || !cmd.DisableFlagParsing {
		t.Fatalf("project job get = %v, %v", cmd, err)
	}
}

func TestWatchSettingsFromFlags(t *testing.T) {
	t.Cleanup(func() { watchOpts = watchOptions{} })
	if err := watchCmd.Flags().Parse([]string{"--concurrency", "2", "--interval", "1m", "--delivery", "Webhook", "--ready-label", "", "--no-merge", "--filter", "label:bug"}); err != nil {
		t.Fatal(err)
	}
	settings, err := watchSettings(watchCmd)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Concurrency != 2 || settings.Interval != "1m" || settings.Delivery != "webhook" || !settings.NoMerge {
		t.Fatalf("settings = %+v", settings)
	}
	if settings.ReadyLabel == nil || *settings.ReadyLabel != "" {
		t.Fatalf("an explicitly empty --ready-label should turn the label off: %v", settings.ReadyLabel)
	}
	if len(settings.Filter) != 1 || settings.Filter[0] != "label:bug" {
		t.Fatalf("filter = %v", settings.Filter)
	}
	watchOpts.delivery = "carrier pigeon"
	if _, err := watchSettings(watchCmd); err == nil {
		t.Fatal("an unknown delivery mode was accepted")
	}
	watchOpts.delivery, watchOpts.interval = "", "soon"
	if _, err := watchSettings(watchCmd); err == nil {
		t.Fatal("an invalid interval was accepted")
	}
	watchOpts.interval = ""
	watchOpts.delivery, watchOpts.browserVisible, watchOpts.browserVision = "Browser", true, true
	settings, err = watchSettings(watchCmd)
	if err != nil || settings.Delivery != "browser" || !settings.BrowserVisible || !settings.BrowserVision {
		t.Fatalf("browser delivery settings = %+v, %v", settings, err)
	}
	watchOpts.delivery = "poll"
	if _, err := watchSettings(watchCmd); err == nil {
		t.Fatal("browser-only switches were accepted for gh polling")
	}
}

func TestWatchTargetsExpandShorthands(t *testing.T) {
	targets, err := watchTargets([]string{"o/r", "https://github.com/o/s"})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[0] != "o/r" {
		t.Fatalf("targets = %v", targets)
	}
}
