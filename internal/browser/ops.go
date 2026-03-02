package browser

import (
	"encoding/json"
	"fmt"

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

// Screenshot captures the full page as PNG bytes.
func (s *Session) Screenshot() ([]byte, error) {
	var buf []byte
	err := s.Run(chromedp.FullScreenshot(&buf, 90))
	return buf, err
}

// EvalJS evaluates JavaScript and returns the result as a string.
func (s *Session) EvalJS(expr string) (string, error) {
	var result string
	err := s.Run(chromedp.Evaluate(expr, &result))
	return result, err
}
