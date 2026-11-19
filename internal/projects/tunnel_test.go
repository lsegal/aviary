package projects

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeTunnel struct {
	url    string
	closed bool
}

func (f *fakeTunnel) URL() string  { return f.url }
func (f *fakeTunnel) Close() error { f.closed = true; return nil }

// endpointConfigurer records the endpoint each webhook was configured with.
type endpointConfigurer struct{ endpoints []string }

func (c *endpointConfigurer) ConfigureWebhook(_ context.Context, _, endpoint, _ string) ([]string, error) {
	c.endpoints = append(c.endpoints, endpoint)
	return nil, nil
}

func newTestSupervisor(tunnel *fakeTunnel, gh webhookConfigurer, logs *[]string) *tunnelSupervisor {
	return &tunnelSupervisor{
		tunnel:      tunnel,
		webhookPath: "/webhook",
		reconciler:  newWebhookReconciler(gh, []string{"o/r"}, tunnel.url+"/webhook", "", func(string, ...interface{}) {}),
		logf:        func(format string, v ...interface{}) { *logs = append(*logs, fmt.Sprintf(format, v...)) },
	}
}

// TestTunnelSupervisorKeepsAHealthyTunnel checks a tunnel that still reaches
// the webhook server is left running and reconciled as before.
func TestTunnelSupervisorKeepsAHealthyTunnel(t *testing.T) {
	tunnel := &fakeTunnel{url: "https://one.ngrok.app"}
	gh := &endpointConfigurer{}
	var logs []string
	s := newTestSupervisor(tunnel, gh, &logs)
	s.probe = func(context.Context, string) error { return nil }
	s.start = func(context.Context) (webhookTunnel, error) {
		t.Fatal("a healthy tunnel was restarted")
		return nil, nil
	}
	s.reconcile(context.Background())
	if tunnel.closed || len(logs) != 0 {
		t.Fatalf("closed=%v logs=%q, want the tunnel left alone", tunnel.closed, logs)
	}
	if len(gh.endpoints) != 1 || gh.endpoints[0] != "https://one.ngrok.app/webhook" {
		t.Fatalf("configured %q, want the running tunnel's endpoint", gh.endpoints)
	}
}

// TestTunnelSupervisorRestartsADeadTunnel checks a tunnel that no longer
// reaches the webhook server -- ngrok died, or its session did not survive the
// machine sleeping -- is replaced, and the webhooks are pointed at the new URL
// in the same pass rather than left posting to a dead one (issue #683).
func TestTunnelSupervisorRestartsADeadTunnel(t *testing.T) {
	dead := &fakeTunnel{url: "https://one.ngrok.app"}
	gh := &endpointConfigurer{}
	var logs []string
	s := newTestSupervisor(dead, gh, &logs)
	s.probe = func(_ context.Context, endpoint string) error {
		if endpoint == "https://one.ngrok.app/webhook" {
			return errors.New("ngrok answered with ERR_NGROK_3200")
		}
		return nil
	}
	replacement := &fakeTunnel{url: "https://two.ngrok.app"}
	s.start = func(context.Context) (webhookTunnel, error) { return replacement, nil }
	s.reconcile(context.Background())
	if !dead.closed {
		t.Fatal("the dead tunnel was not stopped")
	}
	if s.tunnel != replacement {
		t.Fatal("the supervisor did not take the new tunnel")
	}
	if len(gh.endpoints) != 1 || gh.endpoints[0] != "https://two.ngrok.app/webhook" {
		t.Fatalf("configured %q, want the new tunnel's endpoint", gh.endpoints)
	}
	if joined := strings.Join(logs, "\n"); !strings.Contains(joined, "not reachable") || !strings.Contains(joined, "https://two.ngrok.app") {
		t.Fatalf("logs %q, want the restart and the new URL reported", logs)
	}
	if err := s.Close(); err != nil || !replacement.closed {
		t.Fatalf("Close: %v, closed=%v, want the running tunnel stopped", err, replacement.closed)
	}
}

// TestTunnelSupervisorRetriesAFailedRestart checks a tunnel that cannot be
// started again -- the network is still coming back after a wake -- is tried
// again on the next check instead of being given up on.
func TestTunnelSupervisorRetriesAFailedRestart(t *testing.T) {
	dead := &fakeTunnel{url: "https://one.ngrok.app"}
	gh := &endpointConfigurer{}
	var logs []string
	s := newTestSupervisor(dead, gh, &logs)
	s.probe = func(_ context.Context, endpoint string) error {
		if strings.Contains(endpoint, "one") {
			return errors.New("no such host")
		}
		return nil
	}
	starts := 0
	s.start = func(context.Context) (webhookTunnel, error) {
		starts++
		if starts == 1 {
			return nil, errors.New("wait for ngrok tunnel: timed out")
		}
		return &fakeTunnel{url: "https://two.ngrok.app"}, nil
	}
	s.reconcile(context.Background())
	if s.tunnel != nil {
		t.Fatal("a tunnel is recorded after its restart failed")
	}
	s.reconcile(context.Background())
	if starts != 2 || s.tunnel == nil || s.tunnel.URL() != "https://two.ngrok.app" {
		t.Fatalf("starts=%d tunnel=%v, want the restart retried on the next check", starts, s.tunnel)
	}
	if last := gh.endpoints[len(gh.endpoints)-1]; last != "https://two.ngrok.app/webhook" {
		t.Fatalf("last configured endpoint %q, want the restarted tunnel's", last)
	}
}

// TestProbeTunnel checks the probe tells an endpoint that reaches the webhook
// server from one ngrok answers for itself because its agent is gone.
func TestProbeTunnel(t *testing.T) {
	server := httptest.NewServer(WebhookHandler{WebhookPath: "/webhook"})
	defer server.Close()
	if err := probeTunnel(context.Background(), server.URL+"/webhook"); err != nil {
		t.Fatalf("probe of a live webhook server: %v", err)
	}
	offline := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Ngrok-Error-Code", "ERR_NGROK_3200")
		w.WriteHeader(http.StatusNotFound)
	}))
	defer offline.Close()
	if err := probeTunnel(context.Background(), offline.URL+"/webhook"); err == nil || !strings.Contains(err.Error(), "ERR_NGROK_3200") {
		t.Fatalf("probe of an offline ngrok endpoint: %v, want the ngrok error", err)
	}
	offline.Close()
	if err := probeTunnel(context.Background(), offline.URL+"/webhook"); err == nil {
		t.Fatal("probe of an unreachable endpoint succeeded")
	}
}

// TestTunnelSupervisorReportsTunnelHealth checks the supervisor reports webhooks
// as offline from the moment a probe finds the tunnel dead until a probe or
// restart succeeds, so the dashboards stop claiming deliveries are arriving
// while they are lost (issue #687).
func TestTunnelSupervisorReportsTunnelHealth(t *testing.T) {
	dead := &fakeTunnel{url: "https://one.ngrok.app"}
	var logs []string
	s := newTestSupervisor(dead, &endpointConfigurer{}, &logs)
	if !s.online() {
		t.Fatal("a supervisor that has not checked yet reports webhooks offline")
	}
	s.probe = func(_ context.Context, endpoint string) error {
		if strings.Contains(endpoint, "one") {
			return errors.New("ngrok answered with ERR_NGROK_3200")
		}
		return nil
	}
	starts := 0
	s.start = func(context.Context) (webhookTunnel, error) {
		starts++
		if s.online() {
			t.Error("webhooks reported online while the dead tunnel was being restarted")
		}
		if starts == 1 {
			return nil, errors.New("wait for ngrok tunnel: timed out")
		}
		return &fakeTunnel{url: "https://two.ngrok.app"}, nil
	}
	s.reconcile(context.Background())
	if s.online() {
		t.Fatal("webhooks reported online after the restart failed")
	}
	s.reconcile(context.Background())
	if !s.online() {
		t.Fatal("webhooks reported offline after the restart succeeded")
	}
	s.reconcile(context.Background())
	if starts != 2 || !s.online() {
		t.Fatalf("starts=%d online=%v, want the healthy replacement kept and online", starts, s.online())
	}
}

// TestTunnelSupervisorRecoversWhenAProbeSucceeds checks a tunnel that comes
// back on its own is reported online again by the next successful probe.
func TestTunnelSupervisorRecoversWhenAProbeSucceeds(t *testing.T) {
	var logs []string
	s := newTestSupervisor(&fakeTunnel{url: "https://one.ngrok.app"}, &endpointConfigurer{}, &logs)
	s.down.Store(true)
	s.probe = func(context.Context, string) error { return nil }
	s.start = func(context.Context) (webhookTunnel, error) {
		t.Fatal("a healthy tunnel was restarted")
		return nil, nil
	}
	s.reconcile(context.Background())
	if !s.online() {
		t.Fatal("webhooks reported offline after a successful probe")
	}
}

// TestGlorpReportsTunnelHealthToTheDashboards checks the snapshot both
// dashboards render follows the supervisor: online at startup, offline after a
// dead tunnel fails to restart, and online again once a restart succeeds
// (issue #687).
func TestGlorpReportsTunnelHealthToTheDashboards(t *testing.T) {
	var logs []string
	s := newTestSupervisor(&fakeTunnel{url: "https://one.ngrok.app"}, &endpointConfigurer{}, &logs)
	s.probe = func(_ context.Context, endpoint string) error {
		if strings.Contains(endpoint, "one") {
			return errors.New("no such host")
		}
		return nil
	}
	var starts atomic.Int32
	s.start = func(context.Context) (webhookTunnel, error) {
		if starts.Add(1) < 3 {
			return nil, errors.New("wait for ngrok tunnel: timed out")
		}
		return &fakeTunnel{url: "https://two.ngrok.app"}, nil
	}
	reporter := &snapshotReporter{}
	w := &Glorp{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1,
		Issues: &fakeSource{batches: [][]Issue{{}}}, Runner: &fakeRunner{release: make(chan struct{})},
		UseWebhooks: true, fallbackInterval: 5 * time.Millisecond, UI: reporter,
		Webhooks: s.reconcile, WebhookOnline: s.online,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	var states []bool
	for time.Now().Before(deadline) {
		reporter.mu.Lock()
		states = states[:0]
		for _, snapshot := range reporter.snapshots {
			if len(states) == 0 || states[len(states)-1] != snapshot.WebhookOnline {
				states = append(states, snapshot.WebhookOnline)
			}
		}
		reporter.mu.Unlock()
		if len(states) >= 3 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(states) < 3 || !states[0] || states[1] || !states[2] {
		t.Fatalf("WebhookOnline went %v, want online, then offline while the restart failed, then online", states)
	}
}
