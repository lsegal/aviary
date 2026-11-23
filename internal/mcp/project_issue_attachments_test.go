package mcp

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/llm"
)

// project_issue_create and project_issue_comment attach the turn's images and
// videos by default, leave its other files out and name them, add explicit
// attachments, and attach none of the turn's media when told to skip it.
func TestIssueAttachmentsDefaultToTheTurnMedia(t *testing.T) {
	voice := filepath.Join(t.TempDir(), "123_voice.m4a")
	clip := filepath.Join(t.TempDir(), "123_clip.mp4")
	ctx := agent.WithTurnMedia(context.Background(), []string{"data:image/png;base64,Zmlyc3Q=", llm.FileURL(voice), llm.FileURL(clip)})

	files, skipped, err := issueAttachments(ctx, []string{"/extra/diagram.png"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 || filepath.Dir(files[0]) != agent.TurnMediaDir() || files[1] != clip || files[2] != "/extra/diagram.png" {
		t.Errorf("files = %q", files)
	}
	if len(skipped) != 1 || skipped[0] != voice {
		t.Errorf("skipped = %q", skipped)
	}

	files, skipped, err = issueAttachments(ctx, nil, true)
	if err != nil || len(files) != 0 || len(skipped) != 0 {
		t.Errorf("opting out attached %q, skipped %q, %v", files, skipped, err)
	}

	// Explicit attachments are never skipped; GitHub's checks name a bad one.
	files, _, err = issueAttachments(context.Background(), []string{"/notes.txt"}, false)
	if err != nil || len(files) != 1 {
		t.Errorf("explicit attachment: %q, %v", files, err)
	}
	if _, _, err := issueAttachments(context.Background(), []string{"https://example.com/a.png"}, false); err == nil || !strings.Contains(err.Error(), "remote URL") {
		t.Errorf("a remote URL: %v", err)
	}
}

func TestWithSkippedMediaReportsLeftOutFiles(t *testing.T) {
	result, err := withSkippedMedia(map[string]any{"url": "u"}, nil, []string{"/v.m4a"})
	if err != nil || result["skipped_turn_media"] == nil {
		t.Errorf("result = %v, %v", result, err)
	}
	result, _ = withSkippedMedia(map[string]any{"url": "u"}, nil, nil)
	if _, ok := result["skipped_turn_media"]; ok {
		t.Errorf("result names skipped media when none was: %v", result)
	}
}
