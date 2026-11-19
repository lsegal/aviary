package projects

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/lsegal/aviary/internal/projects/ngrok"
)

// tunnelProbeTimeout bounds one check that the ngrok tunnel still reaches the
// webhook server. It is a variable only so tests need not spend it.
var tunnelProbeTimeout = 15 * time.Second

// webhookTunnel is the running ngrok tunnel, as the supervisor sees it.
type webhookTunnel interface {
	URL() string
	Close() error
}

// tunnelSupervisor keeps webhook mode's ngrok tunnel delivering. ngrok is
// started once, and nothing else notices when it dies or its session does not
// survive the machine sleeping, so GitHub would go on posting deliveries to a
// dead URL until glorp restarted (issue #683). The supervisor checks the tunnel
// end to end before every webhook reconciliation -- on each fallback poll and
// whenever the machine wakes -- and starts a new one when it no longer reaches
// the webhook server, pointing the webhooks at the new URL if it changed.
//
// It runs only on the poll loop, as the reconciler it feeds does, so it needs
// no locking, except for down: the dashboards read it from whichever goroutine
// publishes a snapshot.
type tunnelSupervisor struct {
	tunnel      webhookTunnel
	webhookPath string
	reconciler  *webhookReconciler
	start       func(context.Context) (webhookTunnel, error)
	probe       func(context.Context, string) error
	logf        func(string, ...interface{})
	// down records that the last check found the tunnel not reaching the
	// webhook server, or could not start a new one. GitHub deliveries are lost
	// while it lasts, so the dashboards show webhooks as offline (issue #687).
	down atomic.Bool
}

// online reports whether the tunnel was delivering as of the last check. It is
// the run's WebhookOnline hook in push mode.
func (s *tunnelSupervisor) online() bool {
	return !s.down.Load()
}

// reconcile checks the tunnel, then reconciles the webhooks against whichever
// tunnel is now running. It is the run's Webhooks hook in push mode.
func (s *tunnelSupervisor) reconcile(ctx context.Context) {
	s.check(ctx)
	if ctx.Err() == nil {
		s.reconciler.reconcile(ctx)
	}
}

// check replaces the tunnel when it no longer reaches the webhook server. A
// replacement that cannot be started is reported and tried again on the next
// check, so a network that is still coming back up after a wake costs one
// failed attempt rather than the tunnel for good.
func (s *tunnelSupervisor) check(ctx context.Context) {
	if s.tunnel != nil {
		err := s.probe(ctx, s.reconciler.endpoint)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			s.down.Store(false)
			return
		}
		s.down.Store(true)
		s.logf("ngrok tunnel at %s is not reachable (%v); restarting it", s.tunnel.URL(), err)
		_ = s.tunnel.Close()
		s.tunnel = nil
	}
	tunnel, err := s.start(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.down.Store(true)
			s.logf("restart ngrok tunnel: %v; retrying on the next check", err)
		}
		return
	}
	endpoint, err := ngrok.WebhookURL(tunnel.URL(), s.webhookPath)
	if err != nil {
		_ = tunnel.Close()
		s.down.Store(true)
		s.logf("restart ngrok tunnel: %v; retrying on the next check", err)
		return
	}
	s.tunnel = tunnel
	s.down.Store(false)
	if endpoint != s.reconciler.endpoint {
		s.logf("ngrok tunnel restarted at %s; pointing webhooks at it", tunnel.URL())
		s.reconciler.endpoint = endpoint
		// Every target is reported again, so a failure to re-point one is not
		// hidden behind the same failure reported for the old URL.
		s.reconciler.reported = map[string]string{}
	} else {
		s.logf("ngrok tunnel restarted at %s", tunnel.URL())
	}
}

// Close stops whichever tunnel is running when the run ends.
func (s *tunnelSupervisor) Close() error {
	if s.tunnel == nil {
		return nil
	}
	return s.tunnel.Close()
}

// probeTunnel asks the tunnel's public URL for the webhook path and reports
// whether the request reached glorp's webhook server. ngrok answers for an
// endpoint whose agent has gone away itself, marking the response with an
// error code header, so any such answer -- or none at all -- is a dead tunnel.
// The webhook server refuses the GET, which is fine: it only has to answer.
func probeTunnel(ctx context.Context, endpoint string) error {
	ctx, cancel := context.WithTimeout(ctx, tunnelProbeTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	// Keeps ngrok's free-plan browser interstitial out of the way.
	request.Header.Set("ngrok-skip-browser-warning", "1")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if code := response.Header.Get("Ngrok-Error-Code"); code != "" {
		return fmt.Errorf("ngrok answered with %s", code)
	}
	return nil
}
