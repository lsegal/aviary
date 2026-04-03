package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
)

// Navigate goes to the given URL.
func (s *Session) Navigate(url string) error {
	return s.Run(chromedp.Navigate(url))
}

// Click clicks an element matching the CSS selector.
func (s *Session) Click(selector string) error {
	selectorJSON, err := json.Marshal(selector)
	if err != nil {
		return fmt.Errorf("encoding selector: %w", err)
	}

	expr := fmt.Sprintf(`(() => {
		const selector = %s;
		const candidates = Array.from(document.querySelectorAll(selector));
		const target = candidates.find((el) => {
			const cs = window.getComputedStyle(el);
			if (!cs || cs.display === "none" || cs.visibility === "hidden") {
				return false;
			}
			const rect = el.getBoundingClientRect();
			return rect.width > 0 && rect.height > 0;
		});

		if (!target) {
			return "not_found";
		}

		target.click();
		return "clicked";
	})()`, selectorJSON)

	var result string
	if err := s.Run(chromedp.Evaluate(expr, &result)); err != nil {
		return err
	}
	if result != "clicked" {
		return fmt.Errorf("no visible element matched selector %q", selector)
	}
	return nil
}

// Type clears the element matching selector and types text into it.
func (s *Session) Type(selector, text string) error {
	return s.Run(
		chromedp.Clear(selector, chromedp.ByQuery),
		chromedp.SendKeys(selector, text, chromedp.ByQuery),
	)
}

// Fill sets the value of the element matching selector.
func (s *Session) Fill(selector, text string) error {
	return s.Run(chromedp.SetValue(selector, text, chromedp.ByQuery))
}

// WaitVisible waits until an element matching the CSS selector is visible.
func (s *Session) WaitVisible(selector string, timeout time.Duration) error {
	ctx := s.taskCtx
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(s.taskCtx, timeout)
		defer cancel()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return chromedp.Run(ctx, chromedp.WaitVisible(selector, chromedp.ByQuery))
}

// Screenshot captures the full page as PNG bytes.
func (s *Session) Screenshot() ([]byte, error) {
	var buf []byte
	err := s.Run(chromedp.FullScreenshot(&buf, 90))
	return buf, err
}

func formatEvalJSResult(result any) (string, error) {
	if result == nil {
		return "null", nil
	}
	if text, ok := result.(string); ok {
		return text, nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("encoding javascript result: %w", err)
	}
	return string(data), nil
}

// EvalJS evaluates JavaScript and returns a text representation of the result.
func (s *Session) EvalJS(expr string) (string, error) {
	var result any
	if err := s.Run(chromedp.Evaluate(expr, &result)); err != nil {
		return "", err
	}
	return formatEvalJSResult(result)
}
