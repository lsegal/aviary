package cmd

import (
	"bytes"
	"testing"
)

func TestProjectsURL(t *testing.T) {
	tests := []struct {
		server, project, want string
	}{
		{"https://localhost:16677", "", "https://localhost:16677/projects"},
		{"https://localhost:16677/", "lsegal/aviary", "https://localhost:16677/projects/lsegal%2Faviary"},
		{"https://example.test", "Roadmap board", "https://example.test/projects/Roadmap%20board"},
	}
	for _, tt := range tests {
		if got := projectsURL(tt.server, tt.project); got != tt.want {
			t.Errorf("projectsURL(%q, %q) = %q, want %q", tt.server, tt.project, got, tt.want)
		}
	}
}

func TestUICommandOpensBoard(t *testing.T) {
	var opened string
	prevOpen, prevServer := openURL, serverURL
	openURL = func(u string) error { opened = u; return nil }
	serverURL = "https://localhost:16677"
	var out bytes.Buffer
	uiCmd.SetOut(&out)
	t.Cleanup(func() {
		openURL, serverURL = prevOpen, prevServer
		uiCmd.SetOut(nil)
	})

	if err := uiCmd.RunE(uiCmd, []string{"lsegal/aviary"}); err != nil {
		t.Fatalf("ui command failed: %v", err)
	}
	want := "https://localhost:16677/projects/lsegal%2Faviary"
	if opened != want {
		t.Fatalf("opened %q, want %q", opened, want)
	}
	if out.String() != want+"\n" {
		t.Fatalf("printed %q, want %q", out.String(), want+"\n")
	}
}
