package agent

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

func TestPromptMedia_SendsEveryAttachment(t *testing.T) {
	setTestDataDir(t)
	prov := &captureProvider{}
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_multi_media", Name: "multi-media"},
		&config.AgentConfig{Name: "multi-media", Model: "test/model"},
		prov, nil,
	)
	first := "data:image/png;base64,Zmlyc3Q="
	second := "data:image/jpeg;base64,c2Vjb25k"
	notes := "data:text/plain;name=notes.txt;base64," + base64.StdEncoding.EncodeToString([]byte("remember this"))

	done := make(chan struct{}, 1)
	runner.PromptMedia(context.Background(), "look", []string{first, notes, second}, func(e StreamEvent) {
		if e.Type == StreamEventDone || e.Type == StreamEventError || e.Type == StreamEventStop {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		assert.FailNow(t, "timeout")
	}
	runner.Wait()

	assert.NotEmpty(t, prov.requests)
	msgs := prov.requests[0].Messages
	last := msgs[len(msgs)-1]
	assert.Equal(t, []string{first, second}, last.MediaURLs)
	assert.Contains(t, last.Content, "<attachment name=\"notes.txt\">\nremember this\n</attachment>")

	sess, err := NewSessionManager().GetOrCreateNamed(runner.agent.ID, "main")
	assert.NoError(t, err)
	lines, err := store.ReadJSONL[domain.Message](store.SessionPath(runner.agent.ID, sess.ID))
	assert.NoError(t, err)
	var user *domain.Message
	for i := range lines {
		if lines[i].Role == domain.MessageRoleUser {
			user = &lines[i]
		}
	}
	if assert.NotNil(t, user) {
		assert.Equal(t, "look", user.Content)
		assert.Equal(t, []string{first, notes, second}, user.MediaURLs)
	}
}
