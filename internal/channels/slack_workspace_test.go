package channels

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListSlackWorkspaceChannelsWithClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		switch r.URL.Path {
		case "/auth.test":
			assert.Equal(t, "testing-token", r.Form.Get("token"))
			_, _ = w.Write([]byte(`{"ok":true,"team":"Aviary","team_id":"T123","user_id":"U456"}`))
		case "/conversations.list":
			assert.Equal(t, "testing-token", r.Form.Get("token"))
			assert.Equal(t, "public_channel,private_channel", r.Form.Get("types"))
			assert.Equal(t, "true", r.Form.Get("exclude_archived"))
			assert.Equal(t, "200", r.Form.Get("limit"))
			_, _ = w.Write([]byte(`{
				"ok": true,
				"channels": [
					{"id":"C222","name":"zeta","name_normalized":"zeta","is_private":false,"is_member":true,"num_members":8},
					{"id":"C111","name":"alpha","name_normalized":"alpha","is_private":true,"is_member":false,"num_members":3}
				]
			}`))
		default:
			t.Fatalf("unexpected Slack API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	api := slack.New("testing-token", slack.OptionAPIURL(server.URL+"/"))

	info, err := listSlackWorkspaceChannelsWithClient(context.Background(), api)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "T123", info.TeamID)
	assert.Equal(t, "Aviary", info.TeamName)
	assert.Equal(t, "U456", info.BotUserID)
	require.Len(t, info.Channels, 2)
	assert.Equal(t, "alpha", info.Channels[0].Name)
	assert.Equal(t, "C111", info.Channels[0].ID)
	assert.True(t, info.Channels[0].IsPrivate)
	assert.Equal(t, "zeta", info.Channels[1].Name)
}

func TestListSlackWorkspaceChannelsRequiresBotToken(t *testing.T) {
	info, err := ListSlackWorkspaceChannels(context.Background(), "   ")
	require.Error(t, err)
	assert.Nil(t, info)
}

func TestListSlackWorkspaceChannelsPropagatesAuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		switch r.URL.Path {
		case "/auth.test":
			_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
		default:
			t.Fatalf("unexpected Slack API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	api := slack.New("testing-token", slack.OptionAPIURL(server.URL+"/"))

	info, err := listSlackWorkspaceChannelsWithClient(context.Background(), api)
	require.Error(t, err)
	assert.Nil(t, info)
	assert.Contains(t, err.Error(), "auth.test")
}

func TestListSlackWorkspaceChannelsUsesSlackAPIFormEncoding(t *testing.T) {
	var contentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		require.NoError(t, r.ParseForm())
		switch r.URL.Path {
		case "/auth.test":
			_, _ = w.Write([]byte(`{"ok":true,"team":"Aviary","team_id":"T123","user_id":"U456"}`))
		case "/conversations.list":
			_, _ = w.Write([]byte(`{"ok":true,"channels":[]}`))
		}
	}))
	defer server.Close()

	api := slack.New("testing-token", slack.OptionAPIURL(server.URL+"/"))
	_, err := listSlackWorkspaceChannelsWithClient(context.Background(), api)
	require.NoError(t, err)
	assert.Contains(t, contentType, "application/x-www-form-urlencoded")
}
