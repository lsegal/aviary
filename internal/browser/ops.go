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

	result, err := runValue(s, chromedp.Evaluate[string](expr))
	if err != nil {
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
		chromedp.Clear(chromedp.CSS(selector)),
		chromedp.SendKeys(chromedp.CSS(selector), text),
	)
}

// Fill sets the value of the element matching selector.
func (s *Session) Fill(selector, text string) error {
	return s.Run(chromedp.SetValue(chromedp.CSS(selector), text))
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
	return chromedp.Do(ctx, chromedp.WaitVisible(chromedp.CSS(selector)))
}

// Screenshot captures the full page as PNG bytes.
func (s *Session) Screenshot() ([]byte, error) {
	return runValue(s, chromedp.FullScreenshot(90))
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
	result, err := runValue(s, chromedp.Evaluate[any](expr))
	if err != nil {
		return "", err
	}
	return formatEvalJSResult(result)
}
