package browser

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"

	"golang.org/x/net/html"
)

// Board reads a Projects v2 board through its rendered page instead of
// the projects(v2) GraphQL API, which is where the remaining GraphQL calls in
// the watch loop live (issue #276). It satisfies both IssueSource and
// ProjectStateSource for project targets.
//
// Only reads move to the browser. Writing a status back still goes through
// `gh api graphql` in GHCLI.SetIssueStatus: the board page offers no stable
// way to drive a status change, and SetIssueStatus already looks an item's id
// up by issue number when the caller has none, which is exactly the case for
// issues extracted here.
type Board struct {
	// Page hands out the tab a target is read in, one tab per target.
	Page func(name string) (Page, error)
	// Filter and AllIssues mirror the GHCLI fields of the same name so the
	// board page is asked for the same items the GraphQL query selects.
	Filter    string
	AllIssues bool
	// Vision is the bounded screenshot-to-agent fallback, shared with the
	// repository issue source so the per-run cap is one budget for the whole
	// run. Nil (the default, without -browser-vision) means a board that never
	// renders is simply reported as an extraction failure.
	Vision *Vision
	// hydration fills in the dispatch-only fields the board page cannot show,
	// through the same candidate/memo helper the repository issues page uses
	// (issue #395). Nil leaves board items unhydrated, which is what the
	// extraction-only tests want.
	hydration *Hydration
	// Profile is the browser profile directory, named in the signed-out error
	// the same way the repository issues page names it.
	Profile string

	// maxItems caps how many rows are read from one board, matching the cap
	// the GraphQL path applies. maxScrolls caps how many times a virtualized
	// list is scrolled to materialize more rows. settleAttempts and
	// settleDelay bound the wait for the client-rendered board to appear.
	// sleep is a test seam for that wait.
	maxItems       int
	maxScrolls     int
	settleAttempts int
	settleDelay    time.Duration
	sleep          func(context.Context, time.Duration) bool
}

// The settle budget is 15s, matching the one the repository issues page
// settled on in issue #427. Measured against real boards under this reader --
// two public organization boards and a signed-in user board, on cold and warm
// profiles -- GitHub's own client render finished between 0.7s and 1.9s, so
// the old 5s bound left under three times the slowest measured render for a
// slower machine, a colder cache, or a busier GitHub to fit into, and a board
// that was merely slow was reported as one that never rendered. A board that
// has already drawn costs a single harvest and no wait, so the longer budget
// is only ever spent by a board that has not rendered (issue #431).
const (
	defaultBoardMaxItems       = 1000
	defaultBoardMaxScrolls     = 40
	defaultBoardSettleAttempts = 60
	defaultBoardSettleDelay    = 250 * time.Millisecond
)

// NewBoard builds the board reader for a run's browser, giving each
// target its own tab so boards are reloaded rather than reopened.
func NewBoard(browser *Browser, filter string, allIssues bool, vision *Vision, hydration *Hydration) *Board {
	return &Board{
		Page: func(name string) (Page, error) {
			tab, err := browser.Tab(name)
			if err != nil {
				return nil, err
			}
			return tab, nil
		},
		Filter:    filter,
		AllIssues: allIssues,
		Vision:    vision,
		hydration: hydration,
		Profile:   browser.Profile(),
	}
}

// boardDocumentScript returns the page as it currently stands. Extraction is
// done in Go rather than in the page so the selectors are covered by tests
// against captured markup; a script that returned already-parsed rows could
// only ever be exercised against a live board.
const boardDocumentScript = "document.documentElement.outerHTML"

// boardScrollScript advances the board by one viewport and reports whether
// anything actually moved. Projects v2 virtualizes its items, so the ones past
// the viewport are not in the DOM until they are scrolled into it, and the
// caller harvests between scrolls: jumping straight to the bottom moved the
// window past every item in between, which were then never in the DOM to be
// read at all. Every element that has more content than it can show is
// advanced rather than only the largest one, because the table layout has a
// single list but the board layout gives each column a scroller of its own,
// and moving only the biggest of them left every other column stuck on the
// handful of cards that happened to fit (issue #457). Falling back to the
// document covers the layouts that scroll the page itself.
const boardScrollScript = `(function(){
  var moved = false, scrolled = 0;
  function advance(el) {
    var before = el.scrollTop;
    el.scrollTop = Math.min(el.scrollHeight, el.scrollTop + Math.max(el.clientHeight, 1));
    return el.scrollTop > before;
  }
  document.querySelectorAll('*').forEach(function(el){
    if (el.scrollHeight - el.clientHeight <= 40) { return; }
    if (el.clientWidth * el.clientHeight < 2000) { return; }
    scrolled++;
    if (advance(el)) { moved = true; }
  });
  if (scrolled === 0) {
    moved = advance(document.scrollingElement || document.documentElement);
  }
  return moved;
})()`

// boardRow is one item as the board page shows it.
type boardRow struct {
	Number     int
	Repository string
	Title      string
	Status     string
}

func (b *Board) items() int {
	if b.maxItems > 0 {
		return b.maxItems
	}
	return defaultBoardMaxItems
}

func (b *Board) scrolls() int {
	if b.maxScrolls > 0 {
		return b.maxScrolls
	}
	return defaultBoardMaxScrolls
}

func (b *Board) attempts() int {
	if b.settleAttempts > 0 {
		return b.settleAttempts
	}
	return defaultBoardSettleAttempts
}

func (b *Board) delay() time.Duration {
	if b.settleDelay > 0 {
		return b.settleDelay
	}
	return defaultBoardSettleDelay
}

// pause waits between settle attempts, reporting false when the run is being
// shut down so a cancelled poll stops instead of sitting out the whole wait.
func (b *Board) pause(ctx context.Context) bool {
	if b.sleep != nil {
		return b.sleep(ctx, b.delay())
	}
	timer := time.NewTimer(b.delay())
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// ListIssues reports the board's issues with the fields the dispatch gate
// reads: number, repository, title, and the Status column. The body and the
// dependency/sub-issue state are not on the board page, so they are hydrated
// afterwards through the shared helper, on the same budget the repository
// issues page keeps: one targeted REST read per newly seen dispatch candidate,
// memoized for the life of the run, never per tick and never for the whole
// list. The Status column the board owns is not part of that hydration and
// survives it.
func (b *Board) ListIssues(ctx context.Context, target string) ([]core.Issue, error) {
	rows, err := b.readRows(ctx, target)
	if err != nil {
		return nil, err
	}
	issues := make([]core.Issue, 0, len(rows))
	for _, row := range rows {
		issues = append(issues, core.Issue{
			Number:        row.Number,
			Repository:    row.Repository,
			Title:         row.Title,
			ProjectStatus: row.Status,
		})
	}
	if err := b.hydration.hydrateIssues(ctx, target, issues); err != nil {
		return nil, err
	}
	return issues, nil
}

// ProjectState fingerprints the board from the same page read a poll uses, so
// the push-mode probe costs one page load instead of a second network path.
func (b *Board) ProjectState(ctx context.Context, target string) (string, error) {
	rows, err := b.readRows(ctx, target)
	if err != nil {
		return "", err
	}
	return boardRowsFingerprint(rows), nil
}

// boardRowsFingerprint hashes each item's identity and status, ordered
// independently of how the page laid them out so that a reordered board alone
// never reads as a change. It mirrors projectItemsFingerprint on the API path.
func boardRowsFingerprint(rows []boardRow) string {
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, fmt.Sprintf("%s#%d\x00%s", row.Repository, row.Number, row.Status))
	}
	slices.Sort(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// readRows loads a board and extracts every item it shows, scrolling as far as
// it needs to for a virtualized list.
func (b *Board) readRows(ctx context.Context, target string) ([]boardRow, error) {
	parsed, err := core.ParseTarget(target)
	if err != nil {
		return nil, err
	}
	if !parsed.IsProject {
		return nil, fmt.Errorf("project board extraction requires a project target, got %q", target)
	}
	page, err := b.Page(target)
	if err != nil {
		return nil, err
	}
	if err := page.Navigate(boardURL(parsed, b.Filter, b.AllIssues)); err != nil {
		return nil, fmt.Errorf("open project board %s: %w", target, err)
	}
	if status := page.HTTPStatus(); status >= 400 {
		// Private boards are hidden from a signed-out profile the same way
		// private repositories are, so the same question is asked here before
		// the status is blamed on the target (issue #379).
		if signedOutStatus(page, status) {
			return nil, &SignedOutError{URL: boardURL(parsed, b.Filter, b.AllIssues), Profile: b.Profile, Status: status}
		}
		return nil, fmt.Errorf("open project board %s: HTTP %d", target, status)
	}

	// The board is a client-rendered React app, so the first read after
	// navigation usually lands before it has drawn anything. Wait for the
	// board's own markup rather than for a row, so an empty board is reported
	// immediately instead of costing the whole timeout on every poll.
	var rows []boardRow
	rendered := false
	for attempt := 0; attempt < b.attempts(); attempt++ {
		found, ready, err := b.harvest(page)
		if err != nil {
			return nil, fmt.Errorf("read project board %s: %w", target, err)
		}
		if ready {
			rows, rendered = found, true
			break
		}
		if !b.pause(ctx) {
			return nil, ctx.Err()
		}
	}
	// A board that showed nothing is the other face of issue #402: a
	// signed-out profile makes an "@me" filter match nobody, and a board that
	// renders an honestly empty result is indistinguishable from a board with
	// no ready work. Checking before the vision fallback matters as much as
	// checking at all, because no screenshot of a signed-out page can be read
	// into issues.
	if (!rendered || len(rows) == 0) && signedOutPage(page) {
		return nil, &SignedOutError{URL: boardURL(parsed, b.Filter, b.AllIssues), Profile: b.Profile}
	}
	if !rendered {
		// A board that loaded but never drew its own markup is the same
		// category of failure as an unrecognised issues page: GitHub served
		// something, and Aviary could not read it. Reporting it as
		// ErrExtraction lets callers tell it apart from a navigation or
		// HTTP failure the way they already can for the issues page.
		failure := &ExtractionError{
			URL:    boardURL(parsed, b.Filter, b.AllIssues),
			Reason: fmt.Sprintf("the board did not render within %s", time.Duration(b.attempts())*b.delay()),
		}
		if recovered := b.recoverWithVision(ctx, target, page, failure); len(recovered) > 0 {
			return recovered, nil
		}
		return nil, failure
	}

	seen := map[string]bool{}
	collected := make([]boardRow, 0, len(rows))
	appendBoardRows(&collected, seen, rows, b.items())
	for scroll := 0; scroll < b.scrolls() && len(collected) < b.items(); scroll++ {
		moved := false
		if err := page.Eval(boardScrollScript, &moved); err != nil {
			return nil, fmt.Errorf("scroll project board %s: %w", target, err)
		}
		if !moved {
			break
		}
		found, _, err := b.harvest(page)
		if err != nil {
			return nil, fmt.Errorf("read project board %s: %w", target, err)
		}
		if appendBoardRows(&collected, seen, found, b.items()) == 0 {
			break
		}
	}
	return collected, nil
}

// recoverWithVision asks the screenshot fallback to read a board the DOM
// extractor could not, and turns its answer into rows. A board spans
// repositories, so the agent is asked for qualified OWNER/REPO#NUMBER
// references: a bare number could not be turned back into an addressable
// issue. It is asked for each item's Status column alongside the reference,
// because that column is what the --ready-state gate reads: without it a
// recovered board would be addressable but permanently undispatchable (issue
// #398). Recovered rows still carry no title, which nothing gates on. The
// extractor is still expected to be fixed in code.
func (b *Board) recoverWithVision(ctx context.Context, target string, page Page, cause *ExtractionError) []boardRow {
	if b.Vision == nil {
		return nil
	}
	shooter, ok := page.(screenshotter)
	if !ok {
		return nil
	}
	refs := b.Vision.Recover(ctx, target, cause.URL, cause.Error(), shooter.Screenshot, true)
	rows := make([]boardRow, 0, len(refs))
	seen := map[string]bool{}
	statusless := 0
	for _, ref := range refs {
		if ref.Repository == "" || ref.Number <= 0 {
			continue
		}
		key := fmt.Sprintf("%s#%d", ref.Repository, ref.Number)
		if seen[key] {
			continue
		}
		seen[key] = true
		if ref.Status == "" {
			statusless++
		}
		rows = append(rows, boardRow{Number: ref.Number, Repository: ref.Repository, Status: ref.Status})
	}
	if statusless > 0 {
		// An item the screenshot showed no status for cannot be dispatched,
		// because the ready-state gate has nothing to match. Saying so is the
		// point: a recovered board that stays quiet must not look like a
		// recovered board with nothing to do.
		b.Vision.log("browser vision: %d of %d recovered item(s) on %s came back with no Status column, so the ready-state gate will not dispatch them; fix the board extractor rather than relying on this", statusless, len(rows), cause.URL)
	}
	return rows
}

// harvest reads the page once, reporting the items it shows and whether the
// board has rendered at all yet.
func (b *Board) harvest(page Page) ([]boardRow, bool, error) {
	document := ""
	if err := page.Eval(boardDocumentScript, &document); err != nil {
		return nil, false, err
	}
	return parseBoardDocument(document)
}

// appendBoardRows adds the items that have not been seen yet, up to the cap,
// and reports how many were new. Scrolling re-reads rows that were already
// visible, so de-duplication is what makes the scroll loop terminate.
func appendBoardRows(into *[]boardRow, seen map[string]bool, rows []boardRow, limit int) int {
	added := 0
	for _, row := range rows {
		if len(*into) >= limit {
			break
		}
		key := fmt.Sprintf("%s#%d", row.Repository, row.Number)
		if seen[key] {
			continue
		}
		seen[key] = true
		*into = append(*into, row)
		added++
	}
	return added
}

// boardURL builds the board's page URL for any of the three project target
// shapes core.ParseTarget accepts. The table layout is requested explicitly because
// it puts the Status field in a cell of every row, where the board layout only
// implies it through which column a card sits in. The item filter is handed to
// the page rather than applied afterwards, so the browser path selects the same
// items the GraphQL query does.
func boardURL(t core.Target, filter string, allIssues bool) string {
	base := "https://github.com/"
	if t.ProjectOwnerType != "" {
		base += t.ProjectOwnerType + "/" + t.Owner + "/projects/" + t.ProjectID
	} else {
		base += t.Repo + "/projects/" + t.ProjectID
	}
	params := url.Values{"layout": {"table"}}
	if query := core.ProjectItemQuery(filter, allIssues); query != "" {
		params.Set("filterQuery", query)
	}
	return base + "?" + params.Encode()
}

// issueLinkPattern matches the path of a link to an issue. Pull requests live
// under /pull/ and draft board cards link nowhere at all, so both are dropped
// simply by not matching.
var issueLinkPattern = regexp.MustCompile(`^/([^/?#]+)/([^/?#]+)/issues/(\d+)$`)

// parseBoardDocument extracts the board's items from its markup, reporting
// whether the board rendered. The table layout is read first because that is
// the layout the extractor asks for; the board layout is handled as a fallback
// for a board that ignores the layout parameter or is saved on a board view.
func parseBoardDocument(document string) ([]boardRow, bool, error) {
	root, err := html.Parse(strings.NewReader(document))
	if err != nil {
		return nil, false, fmt.Errorf("parse project board page: %w", err)
	}
	// A layout is only preferred over the other when it actually produced
	// items. GitHub's board layout ships unrelated <table> markup of its own
	// (the project's own side panels render one), and taking the first
	// recognised layout meant a board view whose cards were sitting right
	// there was read as an empty table (issue #457).
	tableRows, tableFound := tableBoardRows(root)
	if tableFound && len(tableRows) > 0 {
		return tableRows, true, nil
	}
	columnRows, columnFound := columnBoardRows(root)
	if columnFound && len(columnRows) > 0 {
		return columnRows, true, nil
	}
	if tableFound || columnFound {
		// A board that recognised itself and showed nothing is an empty
		// board, which is reported immediately rather than waited out.
		return nil, true, nil
	}
	return nil, false, nil
}

// tableBoardRows reads the table layout: one row per item, with the Status
// field in a cell of its own.
func tableBoardRows(root *html.Node) ([]boardRow, bool) {
	grid := findNode(root, func(n *html.Node) bool {
		return n.Data == "table" || hasRole(n, "table") || hasRole(n, "grid")
	})
	if grid == nil {
		return nil, false
	}
	rows := []boardRow{}
	for _, node := range collectNodes(grid, func(n *html.Node) bool {
		return n.Data == "tr" || hasRole(n, "row")
	}) {
		row, ok := issueRowFromNode(node)
		if !ok {
			continue
		}
		row.Status = statusFromRow(node)
		rows = append(rows, row)
	}
	return rows, true
}

// columnBoardRows reads the board layout, where an item's status is the column
// it sits in rather than a field on the card.
func columnBoardRows(root *html.Node) ([]boardRow, bool) {
	// GitHub names each column with the status it stands for, in
	// data-board-column. That is preferred over the generic shapes below
	// because a board is wrapped in labelled groups of its own: matching one
	// of those instead would put every card in one column and give them all
	// the wrapper's label as their status (issue #457).
	columns := collectNodes(root, func(n *html.Node) bool {
		return strings.TrimSpace(attrValue(n, "data-board-column")) != ""
	})
	if len(columns) == 0 {
		columns = collectNodes(root, isBoardColumn)
	}
	if len(columns) == 0 {
		return nil, false
	}
	rows := []boardRow{}
	for _, column := range columns {
		status := columnStatus(column)
		for _, card := range collectNodes(column, func(n *html.Node) bool {
			return n.Data == "a" && issueLinkPattern.MatchString(linkPath(attrValue(n, "href")))
		}) {
			row, ok := issueRowFromNode(card)
			if !ok {
				continue
			}
			row.Status = status
			rows = append(rows, row)
		}
	}
	return rows, true
}

func isBoardColumn(n *html.Node) bool {
	if strings.TrimSpace(attrValue(n, "data-board-column")) != "" {
		return true
	}
	if containsFold(attrValue(n, "data-testid"), "column") {
		return true
	}
	return hasRole(n, "group") && strings.TrimSpace(attrValue(n, "aria-label")) != ""
}

// columnStatus names the board column, which is the status of every card in it.
func columnStatus(column *html.Node) string {
	if name := strings.TrimSpace(attrValue(column, "data-board-column")); name != "" {
		return normalizeStatus(name)
	}
	if label := strings.TrimSpace(attrValue(column, "aria-label")); label != "" {
		return normalizeStatus(label)
	}
	title := findNode(column, func(n *html.Node) bool {
		return containsFold(attrValue(n, "data-testid"), "column-title") ||
			n.Data == "h1" || n.Data == "h2" || n.Data == "h3" || n.Data == "h4"
	})
	if title == nil {
		return ""
	}
	return normalizeStatus(nodeText(title))
}

// issueRowFromNode reads the issue a row or card refers to. Anything with no
// issue link — a draft card, a pull request, the header row — is not an issue
// and is reported as such.
func issueRowFromNode(node *html.Node) (boardRow, bool) {
	link := node
	if !(node.Data == "a" && issueLinkPattern.MatchString(linkPath(attrValue(node, "href")))) {
		link = findNode(node, func(n *html.Node) bool {
			return n.Data == "a" && issueLinkPattern.MatchString(linkPath(attrValue(n, "href")))
		})
	}
	if link == nil {
		return boardRow{}, false
	}
	parts := issueLinkPattern.FindStringSubmatch(linkPath(attrValue(link, "href")))
	number, err := strconv.Atoi(parts[3])
	if err != nil || number <= 0 {
		return boardRow{}, false
	}
	title := nodeText(link)
	if title == "" {
		title = strings.TrimSpace(attrValue(link, "aria-label"))
	}
	return boardRow{Number: number, Repository: parts[1] + "/" + parts[2], Title: title}, true
}

// linkPath reduces a link to the path issueLinkPattern matches, so a row that
// uses an absolute github.com URL reads the same as a relative one.
func linkPath(href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if parsed.Host != "" && parsed.Host != "github.com" {
		return ""
	}
	return parsed.Path
}

// statusFromRow reads the row's Status cell. The cell is identified by the
// field name GitHub puts in its test id, label, or class rather than by its
// position, because the columns of a board are user-ordered. The innermost
// match wins: an outer container that merely mentions the field would otherwise
// contribute the whole row's text.
func statusFromRow(row *html.Node) string {
	// GitHub sizes every table cell from a CSS custom property named after
	// the field it holds, and that is the only place the field's name still
	// appears on a row: the cell's classes are hashed CSS-module names and it
	// carries no test id or label of its own. Reading the property is what
	// tells the Status cell apart from the rest (issue #457).
	if cell := findNode(row, func(n *html.Node) bool {
		return strings.EqualFold(cellFieldName(n), "status")
	}); cell != nil {
		// An item with no status has an empty Status cell, which is the
		// answer rather than a reason to keep guessing.
		return statusText(cell)
	}
	best, bestDepth := "", -1
	var walk func(n *html.Node, depth int)
	walk = func(n *html.Node, depth int) {
		if n.Type == html.ElementNode && identifiesStatus(n) {
			if text := statusText(n); text != "" && depth > bestDepth {
				best, bestDepth = text, depth
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child, depth+1)
		}
	}
	walk(row, 0)
	return best
}

// statusAttributes are the attributes GitHub names a field in. A field's own
// name appears in at least one of them on the cell that holds its value.
var statusAttributes = []string{"data-testid", "data-field-name", "data-field", "aria-label", "class", "id"}

// columnWidthPattern pulls a field's name out of the CSS custom property a
// project table sizes its column with (--column-Status-width), which both the
// header cell and every body cell of that column carry.
var columnWidthPattern = regexp.MustCompile(`--column-(.+?)-width`)

// cellFieldName is the board field a table cell holds, or "" for a cell that
// names none.
func cellFieldName(n *html.Node) string {
	match := columnWidthPattern.FindStringSubmatch(attrValue(n, "style"))
	if match == nil {
		return ""
	}
	return strings.ReplaceAll(match[1], "-", " ")
}

func identifiesStatus(n *html.Node) bool {
	if field := cellFieldName(n); field != "" {
		return strings.EqualFold(field, "status")
	}
	if findNode(n, func(c *html.Node) bool {
		return c.Data == "a" && issueLinkPattern.MatchString(linkPath(attrValue(c, "href")))
	}) != nil {
		// The cell holding the item's own link is its title cell, and GitHub
		// labels that cell with the issue's own open/closed state
		// ("<title>, status: open"). That is the issue's state, not the
		// board's Status field, and reading it as one made every row on a
		// real board fail the ready-state gate (issue #457).
		return false
	}
	for _, name := range statusAttributes {
		if containsFold(attrValue(n, name), "status") {
			return true
		}
	}
	return false
}

// statusText reads the value out of a status cell, preferring its text and
// falling back to a label for a cell that renders the value as an icon.
func statusText(n *html.Node) string {
	if text := normalizeStatus(nodeText(n)); text != "" {
		return text
	}
	return normalizeStatus(attrValue(n, "aria-label"))
}

// normalizeStatus collapses a cell's whitespace and drops the field name a
// label like "Status: In Progress" repeats, leaving the value on its own.
func normalizeStatus(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 6 && strings.EqualFold(text[:6], "status") {
		// Only when something is left: a column genuinely named "Status"
		// must not normalize away to nothing.
		if rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text[6:]), ":")); rest != "" {
			return rest
		}
	}
	return text
}

// nodeText is the visible text of a node, with scripts and styles left out so
// an inline payload never reads as a field value.
func nodeText(n *html.Node) string {
	var parts []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			return
		}
		if n.Type == html.TextNode {
			if text := strings.TrimSpace(n.Data); text != "" {
				parts = append(parts, text)
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

func attrValue(n *html.Node, name string) string {
	for _, attr := range n.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func hasRole(n *html.Node, role string) bool {
	return n.Type == html.ElementNode && attrValue(n, "role") == role
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), needle)
}

// findNode returns the first element matching want, in document order.
func findNode(root *html.Node, want func(*html.Node) bool) *html.Node {
	if root.Type == html.ElementNode && want(root) {
		return root
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if found := findNode(child, want); found != nil {
			return found
		}
	}
	return nil
}

// collectNodes returns the outermost elements matching want. A match is not
// descended into, so a row nested in another row's markup is counted once.
func collectNodes(root *html.Node, want func(*html.Node) bool) []*html.Node {
	var found []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n != root && want(n) {
			found = append(found, n)
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

// WatchIssues is browser mode's issue source. The two page readers
// answer for different targets — a repository has an issues page, a project has
// a board — so a run needs both, and each target goes to the one that can read
// it.
type WatchIssues struct {
	// Repos reads a repository's rendered issues page (issue #377).
	Repos core.IssueSource
	// Board reads a project's rendered Projects v2 page (issue #378).
	Board *Board
	// Work answers the per-issue state the run loop watches while an agent is
	// working: whether the ticket closed and what its description now says
	// (issue #469). Neither page reader produces it, and a closed issue drops
	// out of the rendered list rather than reporting itself closed, so it is
	// read through the API the same way posting a comment is.
	Work core.WorkClosureChecker
}

// OriginatingWorkState forwards the active-work check to the API client, so
// browser mode watches an in-flight issue the same way poll mode does.
func (s WatchIssues) OriginatingWorkState(ctx context.Context, repo string, number int) (core.OriginatingWorkState, error) {
	if s.Work == nil {
		return core.OriginatingWorkState{}, fmt.Errorf("read issue #%d state: no work state source configured", number)
	}
	return s.Work.OriginatingWorkState(ctx, repo, number)
}

// StacksEnabled forwards the stacked pull request check to the API client,
// so browser mode stacks a blocked issue the same way poll mode does
// (issue #657).
func (s WatchIssues) StacksEnabled(ctx context.Context, repo string) (bool, error) {
	stacks, ok := s.Work.(core.StackSource)
	if !ok {
		return false, nil
	}
	return stacks.StacksEnabled(ctx, repo)
}

// HydrateIssue forwards the paused watch's issue read to the API client, so
// browser mode wakes paused work the page readers no longer list the same way
// poll mode does (issue #769).
func (s WatchIssues) HydrateIssue(ctx context.Context, repo string, issue *core.Issue) error {
	hydrator, ok := s.Work.(IssueHydrator)
	if !ok {
		return fmt.Errorf("read issue #%d: no issue source configured", issue.Number)
	}
	return hydrator.HydrateIssue(ctx, repo, issue)
}

func (s WatchIssues) ListIssues(ctx context.Context, target string) ([]core.Issue, error) {
	if core.IsProjectTarget(target) {
		return s.Board.ListIssues(ctx, target)
	}
	return s.Repos.ListIssues(ctx, target)
}
