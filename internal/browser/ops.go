package browser

import (
	"github.com/chromedp/chromedp"
)

// Navigate goes to the given URL.
func (s *Session) Navigate(url string) error {
	return s.Run(chromedp.Navigate(url))
}

// Click clicks an element matching the CSS selector.
func (s *Session) Click(selector string) error {
	return s.Run(chromedp.Click(selector, chromedp.ByQuery))
}

// Type clears the element matching selector and types text into it.
func (s *Session) Type(selector, text string) error {
	return s.Run(
		chromedp.Clear(selector, chromedp.ByQuery),
		chromedp.SendKeys(selector, text, chromedp.ByQuery),
	)
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
