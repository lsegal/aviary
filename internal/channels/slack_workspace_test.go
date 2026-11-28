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

func TestListSlackWorkspaceChannelsTrimsTokenAndUsesClientFactory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "xoxb-token", r.Form.Get("token"))
		switch r.URL.Path {
		case "/auth.test":
			_, _ = w.Write([]byte(`{"ok":true,"team":" Aviary ","team_id":" T1 ","user_id":" U1 "}`))
		case "/conversations.list":
			_, _ = w.Write([]byte(`{"ok":true,"channels":[{"id":"C1","name":"general"}]}`))
		default:
			t.Errorf("unexpected Slack API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	var gotToken string
	orig := newSlackWorkspaceClient
	newSlackWorkspaceClient = func(botToken string) *slack.Client {
		gotToken = botToken
		return slack.New(botToken, slack.OptionAPIURL(server.URL+"/"))
	}
	t.Cleanup(func() { newSlackWorkspaceClient = orig })

	info, err := ListSlackWorkspaceChannels(context.Background(), "  xoxb-token\n")
	require.NoError(t, err)
	assert.Equal(t, "xoxb-token", gotToken)
	assert.Equal(t, "T1", info.TeamID)
	assert.Equal(t, "Aviary", info.TeamName)
	assert.Equal(t, "U1", info.BotUserID)
	require.Len(t, info.Channels, 1)
	assert.Equal(t, "general", info.Channels[0].Name)
}

func TestListSlackWorkspaceChannelsMissingTokenSkipsClient(t *testing.T) {
	orig := newSlackWorkspaceClient
	newSlackWorkspaceClient = func(string) *slack.Client {
		t.Fatal("client must not be created without a token")
		return nil
	}
	t.Cleanup(func() { newSlackWorkspaceClient = orig })

	info, err := ListSlackWorkspaceChannels(context.Background(), "")
	require.EqualError(t, err, "slack bot token is required")
	assert.Nil(t, info)
}

func TestListSlackWorkspaceChannelsWithClientRequiresClient(t *testing.T) {
	info, err := listSlackWorkspaceChannelsWithClient(context.Background(), nil)
	require.EqualError(t, err, "slack client is required")
	assert.Nil(t, info)
}

func TestListSlackWorkspaceChannelsPropagatesConversationsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth.test":
			_, _ = w.Write([]byte(`{"ok":true,"team":"Aviary","team_id":"T1","user_id":"U1"}`))
		case "/conversations.list":
			_, _ = w.Write([]byte(`{"ok":false,"error":"missing_scope"}`))
		default:
			t.Errorf("unexpected Slack API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	api := slack.New("testing-token", slack.OptionAPIURL(server.URL+"/"))
	info, err := listSlackWorkspaceChannelsWithClient(context.Background(), api)
	require.Error(t, err)
	assert.Nil(t, info)
	assert.Contains(t, err.Error(), "slack conversations.list failed")
	assert.Contains(t, err.Error(), "missing_scope")
}

func TestListSlackWorkspaceChannelsFollowsCursorsAndNormalizesChannels(t *testing.T) {
	var cursors []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		switch r.URL.Path {
		case "/auth.test":
			_, _ = w.Write([]byte(`{"ok":true,"team":"Aviary","team_id":"T1","user_id":"U1"}`))
		case "/conversations.list":
			cursor := r.Form.Get("cursor")
			cursors = append(cursors, cursor)
			switch cursor {
			case "":
				_, _ = w.Write([]byte(`{
					"ok": true,
					"channels": [
						{"id":"C3","name":"Beta","is_member":true,"num_members":4},
						{"id":"  ","name":"blank-id"},
						{"id":"C9","name":"  ","name_normalized":" fallback "}
					],
					"response_metadata": {"next_cursor":"page2"}
				}`))
			case "page2":
				_, _ = w.Write([]byte(`{
					"ok": true,
					"channels": [
						{"id":"C2","name":"beta","is_private":true,"is_archived":true},
						{"id":"C1","name":"","name_normalized":""}
					],
					"response_metadata": {"next_cursor":""}
				}`))
			default:
				t.Errorf("unexpected cursor %q", cursor)
			}
		default:
			t.Errorf("unexpected Slack API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	api := slack.New("testing-token", slack.OptionAPIURL(server.URL+"/"))
	info, err := listSlackWorkspaceChannelsWithClient(context.Background(), api)
	require.NoError(t, err)
	assert.Equal(t, []string{"", "page2"}, cursors)

	// The blank-ID channel is dropped; nameless channels fall back to the
	// normalized name, then the ID; equal names (case-insensitive) sort by ID.
	assert.Equal(t, []SlackWorkspaceChannel{
		{ID: "C2", Name: "beta", IsPrivate: true, IsArchived: true},
		{ID: "C3", Name: "Beta", IsMember: true, NumMembers: 4},
		{ID: "C1", Name: "C1"},
		{ID: "C9", Name: "fallback", NameNormalized: "fallback"},
	}, info.Channels)
}
