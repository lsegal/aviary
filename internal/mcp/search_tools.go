package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/browser"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type webSearchArgs struct {
	Query string `json:"query"`
	Count int    `json:"count,omitempty"` // number of results; default 10, max 20
}

type searchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

func registerSearchTools(s *sdkmcp.Server) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name: "web_search",
		Description: "Search the web for a query and return a list of results with titles, URLs, and descriptions. " +
			"Uses the Brave Search API if a 'brave:api_key' credential has been set via auth_set; " +
			"otherwise falls back to searching DuckDuckGo via the browser.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args webSearchArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "search", "tool", "web_search", "query", args.Query)
		if args.Query == "" {
			return nil, struct{}{}, fmt.Errorf("query is required")
		}
		count := args.Count
		if count <= 0 {
			count = 10
		}
		if count > 20 {
			count = 20
		}

		// Prefer Brave Search API when an API key is configured.
		st, err := authStore()
		if err == nil {
			if apiKey, err := st.Get("brave:api_key"); err == nil && apiKey != "" {
				results, err := braveSearch(ctx, apiKey, args.Query, count)
				if err != nil {
					return nil, struct{}{}, fmt.Errorf("brave search: %w", err)
				}
				slog.Info("mcp: web_search completed", "component", "search", "backend", "brave", "results", len(results))
				return jsonResult(results)
			}
		}

		// Fall back to browser-based DuckDuckGo search.
		d := GetDeps()
		if d.Browser == nil {
			return nil, struct{}{}, fmt.Errorf(
				"no search backend available: store your Brave Search API key with " +
					"auth_set(name='brave:api_key', value='<key>'), or ensure the browser is configured")
		}
		results, err := browserSearch(ctx, d.Browser, args.Query, count)
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("browser search: %w", err)
		}
		slog.Info("mcp: web_search completed", "component", "search", "backend", "browser", "results", len(results))
		return jsonResult(results)
	})
}

// braveSearch queries the Brave Search API and returns structured results.
func braveSearch(ctx context.Context, apiKey, query string, count int) ([]searchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.search.brave.com/res/v1/web/search", nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Set("q", query)
	q.Set("count", fmt.Sprintf("%d", count))
	req.URL.RawQuery = q.Encode()
	req.Header.Set("X-Subscription-Token", apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	results := make([]searchResult, 0, len(payload.Web.Results))
	for _, r := range payload.Web.Results {
		results = append(results, searchResult{
			Title:       r.Title,
			URL:         r.URL,
			Description: r.Description,
		})
	}
	return results, nil
}

// browserSearch navigates to DuckDuckGo Lite in a new browser tab, extracts
// results via JavaScript, then closes the tab.
func browserSearch(ctx context.Context, br *browser.Manager, query string, count int) ([]searchResult, error) {
	searchURL := "https://lite.duckduckgo.com/lite/?q=" + url.QueryEscape(query)

	// Allow more time for page load + JS evaluation.
	opCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Open a blank tab so we navigate only once (via chromedp, which waits for load).
	tabID, err := br.Open(opCtx, "about:blank")
	if err != nil {
		return nil, fmt.Errorf("opening tab: %w", err)
	}
	defer br.CloseTab(tabID) //nolint:errcheck

	// Navigate and wait for the page to finish loading.
	if err := br.Navigate(opCtx, tabID, searchURL); err != nil {
		return nil, fmt.Errorf("navigating to search page: %w", err)
	}

	// Extract results.  DuckDuckGo Lite renders organic results as consecutive
	// <tr> rows: the first contains an <a class="result-link"> with the title
	// and destination URL; the next sibling contains a <td class="result-snippet">
	// with the description snippet.
	extractJS := fmt.Sprintf(`(() => {
		const results = [];
		const links = Array.from(document.querySelectorAll('a.result-link'));
		for (let i = 0; i < Math.min(%d, links.length); i++) {
			const a = links[i];
			const row = a.closest('tr');
			const snippetTd = row && row.nextElementSibling
				? row.nextElementSibling.querySelector('td.result-snippet')
				: null;
			results.push({
				title: a.textContent.trim(),
				url: a.href,
				description: snippetTd ? snippetTd.textContent.trim() : ''
			});
		}
		return JSON.stringify(results);
	})()`, count)

	raw, err := br.EvalJS(opCtx, tabID, extractJS)
	if err != nil {
		return nil, fmt.Errorf("extracting results: %w", err)
	}

	var items []searchResult
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("parsing search results (raw=%q): %w", raw, err)
	}
	return items, nil
}
