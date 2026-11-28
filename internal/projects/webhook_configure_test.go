package projects

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// apiCall is one `gh api` request recorded by fakeWebhookAPI.
type apiCall struct {
	method string
	path   string
	body   string
}

// apiReply is a canned `gh api` response.
type apiReply struct {
	output string
	err    error
}

// fakeWebhookAPI answers `gh api` requests from canned replies keyed by
// "METHOD path", recording every request it receives.
type fakeWebhookAPI struct {
	replies map[string]apiReply
	calls   []apiCall
}

func (f *fakeWebhookAPI) run(_ context.Context, args []string, body ...string) ([]byte, error) {
	call := apiCall{method: "GET", path: args[1]}
	for i := 2; i < len(args); i++ {
		if args[i] == "--method" && i+1 < len(args) {
			call.method = args[i+1]
		}
	}
	if len(body) > 0 {
		call.body = body[0]
	}
	f.calls = append(f.calls, call)
	reply, ok := f.replies[call.method+" "+call.path]
	if !ok {
		return nil, errors.New("unexpected request " + call.method + " " + call.path)
	}
	return []byte(reply.output), reply.err
}

func (f *fakeWebhookAPI) callsWithMethod(method string) []apiCall {
	var matched []apiCall
	for _, call := range f.calls {
		if call.method == method {
			matched = append(matched, call)
		}
	}
	return matched
}

func decodeBody(t *testing.T, body string) map[string]interface{} {
	t.Helper()
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("request body %q: %v", body, err)
	}
	return decoded
}

const testEndpoint = "https://tunnel.example/webhook"

// A Discussions target sharing the repository target's hook widens it with
// one PATCH carrying the sorted union of events instead of creating a second
// hook.
func TestConfigureWebhookWidensExistingHookEvents(t *testing.T) {
	fake := &fakeWebhookAPI{replies: map[string]apiReply{
		"GET repos/o/r/hooks":     {output: `[{"id":7,"events":["ping","issues"],"config":{"url":"` + testEndpoint + `"}}]`},
		"PATCH repos/o/r/hooks/7": {output: `{}`},
	}}
	created, err := GHCLI{apiCommand: fake.run}.ConfigureWebhook(context.Background(), "https://github.com/o/r/discussions", testEndpoint, "s3cret")
	if err != nil || len(created) != 0 {
		t.Fatalf("ConfigureWebhook() = %v, %v; want the existing hook widened, nothing created", created, err)
	}
	patches := fake.callsWithMethod("PATCH")
	if len(patches) != 1 || len(fake.callsWithMethod("POST")) != 0 {
		t.Fatalf("calls = %#v, want exactly one PATCH and no POST", fake.calls)
	}
	events := decodeBody(t, patches[0].body)["events"]
	if !reflect.DeepEqual(events, []interface{}{"discussion", "issues", "ping"}) {
		t.Fatalf("PATCH events = %#v, want the sorted union", events)
	}
}

func TestConfigureWebhookReportsWidenFailure(t *testing.T) {
	denied := errors.New("HTTP 403: Resource not accessible by integration")
	fake := &fakeWebhookAPI{replies: map[string]apiReply{
		"GET repos/o/r/hooks":     {output: `[{"id":7,"events":["issues"],"config":{"url":"` + testEndpoint + `"}}]`},
		"PATCH repos/o/r/hooks/7": {err: denied},
	}}
	created, err := GHCLI{apiCommand: fake.run}.ConfigureWebhook(context.Background(), "https://github.com/o/r/discussions", testEndpoint, "")
	if !errors.Is(err, denied) || len(created) != 0 {
		t.Fatalf("ConfigureWebhook() = %v, %v; want the PATCH failure", created, err)
	}
	if want := "add discussion, ping events to webhooks for o/r discussions"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want it to contain %q", err, want)
	}
	if strings.Contains(err.Error(), "admin:org_hook") {
		t.Fatalf("repository webhook error = %v, want no organization access hint", err)
	}
}

// A missing hook is created with the spec's events and the secret, and a
// stale ngrok hook left by an earlier tunnel is removed.
func TestConfigureWebhookCreatesHookAndRemovesStaleNgrokHook(t *testing.T) {
	fake := &fakeWebhookAPI{replies: map[string]apiReply{
		"GET repos/o/r/hooks":      {output: `[{"id":3,"config":{"url":"https://abc.ngrok-free.app/webhook"}},{"id":4,"config":{"url":"https://ci.example/hook"}}]`},
		"DELETE repos/o/r/hooks/3": {output: ``},
		"POST repos/o/r/hooks":     {output: `{"id":9}`},
	}}
	created, err := GHCLI{apiCommand: fake.run}.ConfigureWebhook(context.Background(), "o/r", testEndpoint, "s3cret")
	if err != nil || !reflect.DeepEqual(created, []string{"o/r"}) {
		t.Fatalf("ConfigureWebhook() = %v, %v; want o/r created", created, err)
	}
	if deletes := fake.callsWithMethod("DELETE"); len(deletes) != 1 || deletes[0].path != "repos/o/r/hooks/3" {
		t.Fatalf("DELETE calls = %#v, want only the ngrok hook removed", deletes)
	}
	posts := fake.callsWithMethod("POST")
	if len(posts) != 1 {
		t.Fatalf("POST calls = %#v, want one", posts)
	}
	body := decodeBody(t, posts[0].body)
	if body["name"] != "web" || body["active"] != true {
		t.Fatalf("POST body = %#v", body)
	}
	if !reflect.DeepEqual(body["events"], []interface{}{"issues", "pull_request", "push", "ping", "issue_comment", "pull_request_review", "pull_request_review_comment"}) {
		t.Fatalf("POST events = %#v", body["events"])
	}
	config := body["config"].(map[string]interface{})
	want := map[string]interface{}{"url": testEndpoint, "content_type": "json", "insecure_ssl": "0", "secret": "s3cret"}
	if !reflect.DeepEqual(config, want) {
		t.Fatalf("POST config = %#v, want %#v", config, want)
	}
}

func TestConfigureWebhookOmitsEmptySecret(t *testing.T) {
	fake := &fakeWebhookAPI{replies: map[string]apiReply{
		"GET repos/o/r/hooks":  {output: `[]`},
		"POST repos/o/r/hooks": {output: `{"id":9}`},
	}}
	if _, err := (GHCLI{apiCommand: fake.run}).ConfigureWebhook(context.Background(), "o/r", testEndpoint, ""); err != nil {
		t.Fatal(err)
	}
	config := decodeBody(t, fake.callsWithMethod("POST")[0].body)["config"].(map[string]interface{})
	if _, ok := config["secret"]; ok {
		t.Fatalf("POST config = %#v, want no secret", config)
	}
}

func TestConfigureWebhookSpecErrors(t *testing.T) {
	notFound := errors.New("HTTP 404: Not Found")
	tests := []struct {
		name    string
		replies map[string]apiReply
		want    string
		wrapped error
	}{
		{
			name:    "list fails",
			replies: map[string]apiReply{"GET repos/o/r/hooks": {err: notFound}},
			want:    "list webhooks for o/r: HTTP 404",
			wrapped: notFound,
		},
		{
			name:    "list is not JSON",
			replies: map[string]apiReply{"GET repos/o/r/hooks": {output: `not json`}},
			want:    "decode webhooks for o/r",
		},
		{
			name: "stale ngrok hook cannot be removed",
			replies: map[string]apiReply{
				"GET repos/o/r/hooks":      {output: `[{"id":3,"config":{"url":"https://abc.ngrok.io/webhook"}}]`},
				"DELETE repos/o/r/hooks/3": {err: notFound},
			},
			want:    "remove old ngrok webhook 3 from webhooks for o/r",
			wrapped: notFound,
		},
		{
			name: "create fails",
			replies: map[string]apiReply{
				"GET repos/o/r/hooks":  {output: `[]`},
				"POST repos/o/r/hooks": {err: notFound},
			},
			want:    "create webhooks for o/r: HTTP 404",
			wrapped: notFound,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeWebhookAPI{replies: test.replies}
			created, err := GHCLI{apiCommand: fake.run}.configureWebhookSpec(context.Background(), repositoryWebhookSpec("o/r"), testEndpoint, "")
			if err == nil || created {
				t.Fatalf("configureWebhookSpec() = %v, %v; want an error", created, err)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want it to contain %q", err, test.want)
			}
			if test.wrapped != nil && !errors.Is(err, test.wrapped) {
				t.Fatalf("error = %v, want it to wrap %v", err, test.wrapped)
			}
		})
	}
}

func TestConfigureWebhookRejectsInvalidTarget(t *testing.T) {
	fake := &fakeWebhookAPI{}
	if _, err := (GHCLI{apiCommand: fake.run}).ConfigureWebhook(context.Background(), "not a target", testEndpoint, ""); err == nil {
		t.Fatal("ConfigureWebhook() accepted an invalid target")
	}
	if len(fake.calls) != 0 {
		t.Fatalf("calls = %#v, want none for an invalid target", fake.calls)
	}
}

func TestConfigureWebhookReportsSpecLookupFailure(t *testing.T) {
	fake := &fakeWebhookAPI{replies: map[string]apiReply{"GET users/o": {err: errors.New("HTTP 502")}}}
	_, err := GHCLI{apiCommand: fake.run}.ConfigureWebhook(context.Background(), "https://github.com/o/r/projects/2", testEndpoint, "")
	if err == nil || !strings.Contains(err.Error(), "identify project owner o") {
		t.Fatalf("error = %v, want the owner lookup failure", err)
	}
	if len(fake.callsWithMethod("POST")) != 0 {
		t.Fatalf("calls = %#v, want no webhook created", fake.calls)
	}
}

// When only some of an organization board's hooks can be configured, the
// rest are still created and the error is reported as partial, carrying the
// organization access hint for the hook that failed.
func TestConfigureWebhookReportsPartialFailure(t *testing.T) {
	notFound := errors.New("HTTP 404: Not Found")
	fake := &fakeWebhookAPI{replies: map[string]apiReply{
		"GET orgs/acme/hooks":     {err: notFound},
		"GET repos/acme/a/hooks":  {output: `[]`},
		"POST repos/acme/a/hooks": {output: `{"id":1}`},
		"GET repos/acme/b/hooks":  {output: `[{"id":2,"events":["*"],"config":{"url":"` + testEndpoint + `"}}]`},
	}}
	gh := GHCLI{
		apiCommand: fake.run,
		runCommand: func(context.Context, ...string) ([]byte, error) {
			return orgProjectItemsResponse("acme/b", "acme/a"), nil
		},
	}
	created, err := gh.ConfigureWebhook(context.Background(), "https://github.com/orgs/acme/projects/1", testEndpoint, "")
	if !reflect.DeepEqual(created, []string{"acme/a project issues"}) {
		t.Fatalf("created = %v, want only acme/a's issue hook", created)
	}
	if !errors.Is(err, errWebhookPartiallyConfigured) || !errors.Is(err, notFound) {
		t.Fatalf("error = %v, want a partial failure wrapping the 404", err)
	}
	for _, want := range []string{"(1 of 3)", "list webhooks for organization project acme", "admin:org_hook"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to contain %q", err, want)
		}
	}
	if len(fake.callsWithMethod("PATCH")) != 0 {
		t.Fatalf("calls = %#v, want the wildcard hook left alone", fake.calls)
	}
}

// When every hook fails the error is the failure itself, not a partial one.
func TestConfigureWebhookReportsTotalFailure(t *testing.T) {
	denied := errors.New("HTTP 403: Forbidden")
	fake := &fakeWebhookAPI{replies: map[string]apiReply{"GET repos/o/r/hooks": {err: denied}}}
	created, err := GHCLI{apiCommand: fake.run}.ConfigureWebhook(context.Background(), "o/r", testEndpoint, "")
	if len(created) != 0 || !errors.Is(err, denied) || errors.Is(err, errWebhookPartiallyConfigured) {
		t.Fatalf("ConfigureWebhook() = %v, %v; want the 403 reported as a total failure", created, err)
	}
}

func TestProjectOwnerType(t *testing.T) {
	tests := []struct {
		name      string
		ownerType string
		reply     *apiReply
		want      string
		wantErr   string
	}{
		{name: "known owner type skips the API", ownerType: "orgs", want: "orgs"},
		{name: "organization", reply: &apiReply{output: `{"login":"o","type":"Organization"}`}, want: "orgs"},
		{name: "user", reply: &apiReply{output: `{"login":"o","type":"User"}`}, want: "users"},
		{name: "lookup fails", reply: &apiReply{err: errors.New("HTTP 404")}, wantErr: "identify project owner o: HTTP 404"},
		{name: "response is not JSON", reply: &apiReply{output: `<html>`}, wantErr: "decode project owner o"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			replies := map[string]apiReply{}
			if test.reply != nil {
				replies["GET users/o"] = *test.reply
			}
			fake := &fakeWebhookAPI{replies: replies}
			target, err := parseTarget("https://github.com/o/r/projects/2")
			if err != nil {
				t.Fatal(err)
			}
			target.ProjectOwnerType = test.ownerType
			got, err := GHCLI{apiCommand: fake.run}.projectOwnerType(context.Background(), target)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("projectOwnerType() = %q, %v; want error containing %q", got, err, test.wantErr)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("projectOwnerType() = %q, %v; want %q", got, err, test.want)
			}
			if wantCalls := map[bool]int{true: 1, false: 0}[test.reply != nil]; len(fake.calls) != wantCalls {
				t.Fatalf("calls = %#v, want %d", fake.calls, wantCalls)
			}
		})
	}
}
