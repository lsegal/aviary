package agent

import (
	"fmt"
	"sync"
	"testing"
)

func TestSessionDeliveryConcurrentRegistrationAndDispatch(_ *testing.T) {
	const iterations = 500
	const workers = 4

	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				RegisterSessionDelivery("concurrent-agent", "concurrent-text", "signal", fmt.Sprintf("%d-%d", worker, i%16), func(string) {})
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				deliverToSession("concurrent-agent", "concurrent-text", "message")
			}
		}()
	}
	wg.Wait()
}

func TestSessionMediaDeliveryConcurrentRegistrationAndDispatch(_ *testing.T) {
	const iterations = 500
	const workers = 4

	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				RegisterSessionMediaDelivery("concurrent-agent", "concurrent-media", "slack", fmt.Sprintf("%d-%d", worker, i%16), func(string, string) {})
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				DeliverMediaToSession("concurrent-agent", "concurrent-media", "caption", "image.png")
			}
		}()
	}
	wg.Wait()
}
