package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// Limits on what the Code Loop agent's repository and site reads return.
const (
	// readBodyLimit caps a read's body; a cut keeps a log's tail, where its
	// failure is, and every other body's head.
	readBodyLimit = 64 << 10
	// readTimeout bounds one read, so a slow API or site never holds a chat
	// turn.
	readTimeout = time.Minute
	// siteHostsTTL is how long the watched repositories' site hosts are
	// reused before they are read from GitHub again.
	siteHostsTTL = 10 * time.Minute
)

// readMediaTypes are the Accept headers a GitHub read may ask for.
var readMediaTypes = map[string]string{
	"diff":  "application/vnd.github.diff",
	"patch": "application/vnd.github.patch",
	"raw":   "application/vnd.github.raw",
}

// siteHostCache holds the hosts project_site_fetch may read.
type siteHostCache struct {
	mu    sync.Mutex
	key   string
	hosts []string
	at    time.Time
}

// watchedRepositories are the repositories the Code Loop agent may read: the
// watched repository targets and every repository a job works in, which
// covers the repositories of watched project boards.
func (m *Manager) watchedRepositories() []string {
	state := m.display()
	var repositories []string
	add := func(repository string) {
		if core.ValidRepo(repository) && !slices.ContainsFunc(repositories, func(r string) bool { return strings.EqualFold(r, repository) }) {
			repositories = append(repositories, repository)
		}
	}
	for _, target := range state.Targets {
		add(target)
	}
	for _, job := range state.Jobs {
		add(job.Repository)
		add(job.Target)
	}
	return repositories
}

// watchedRepository returns repository as watched, or the only watched
// repository when repository is empty.
func (m *Manager) watchedRepository(repository string) (string, error) {
	watched := m.watchedRepositories()
	repository = strings.TrimSpace(repository)
	if repository == "" {
		if len(watched) == 1 {
			return watched[0], nil
		}
		if len(watched) == 0 {
			return "", errors.New("no repositories are watched")
		}
		return "", fmt.Errorf("name the repository: %s are watched", strings.Join(watched, ", "))
	}
	for _, r := range watched {
		if strings.EqualFold(r, repository) {
			return r, nil
		}
	}
	return "", fmt.Errorf("%s is not a watched repository; only %s can be read", repository, strings.Join(watched, ", "))
}

// readGH is the gh the agent's reads go through: the instance's own auth,
// bounded by readTimeout.
func (m *Manager) readGH() GHCLI {
	return GHCLI{Binary: "gh", Timeout: readTimeout, runCommand: m.ghRunCommand}
}

// GitHubRead is project_github_read: one GET of the GitHub REST API under a
// watched repository, named by a path under repos/OWNER/REPO or by a GitHub
// URL. media asks for a diff, patch or raw body instead of JSON.
func (m *Manager) GitHubRead(ctx context.Context, repository, path, link, media string) (map[string]any, error) {
	if link = strings.TrimSpace(link); link != "" {
		if strings.TrimSpace(path) != "" {
			return nil, errors.New("pass a url or a path, not both")
		}
		linkRepository, linkPath, err := githubURLPath(link)
		if err != nil {
			return nil, err
		}
		if repository != "" && !strings.EqualFold(repository, linkRepository) {
			return nil, fmt.Errorf("the url is in %s, not %s", linkRepository, repository)
		}
		repository, path = linkRepository, linkPath
	}
	repository, err := m.watchedRepository(repository)
	if err != nil {
		return nil, err
	}
	path, err = cleanReadPath(path)
	if err != nil {
		return nil, err
	}
	media = strings.ToLower(strings.TrimSpace(media))
	if media == "" && strings.HasPrefix(path, "contents/") {
		// A file's text, rather than its base64 encoding; a directory still
		// lists as JSON.
		media = "raw"
	}
	endpoint := "repos/" + repository
	if path != "" {
		endpoint += "/" + path
	}
	args := []string{"api", "--method", "GET", endpoint}
	if media != "" {
		accept, ok := readMediaTypes[media]
		if !ok {
			return nil, fmt.Errorf("unknown media %q: use diff, patch or raw", media)
		}
		args = append(args, "-H", "Accept: "+accept)
	}
	m.logf("Code Loop agent read GitHub %s", endpoint)
	output, err := m.readGH().Run(ctx, args...)
	if err != nil {
		if rateLimited(err) {
			m.logf("Code Loop agent read GitHub %s: rate limited", endpoint)
			return nil, fmt.Errorf("GitHub's API rate limit is used up; wait for it to reset before reading again: %w", err)
		}
		return nil, fmt.Errorf("read %s: %w", endpoint, err)
	}
	body, truncated := limitBody(string(output), strings.HasSuffix(path, "/logs"))
	result := map[string]any{"repository": repository, "path": path, "truncated": truncated}
	var decoded any
	if !truncated && json.Unmarshal(output, &decoded) == nil {
		result["data"] = decoded
	} else {
		result["text"] = body
	}
	return result, nil
}

// cleanReadPath checks a path under repos/OWNER/REPO: relative, with no step
// out of the repository.
func cleanReadPath(path string) (string, error) {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if strings.ContainsAny(path, " \t\r\n\\") {
		return "", fmt.Errorf("invalid path %q", path)
	}
	lower := strings.ToLower(path)
	if strings.Contains(lower, "%2e") || strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
		return "", fmt.Errorf("invalid path %q: no encoded dots or slashes", path)
	}
	route, _, _ := strings.Cut(path, "?")
	for _, segment := range strings.Split(route, "/") {
		if segment == "." || segment == ".." {
			return "", fmt.Errorf("invalid path %q: it must stay inside the repository", path)
		}
	}
	return path, nil
}

// limitBody cuts body to readBodyLimit, keeping its tail when tail is set.
func limitBody(body string, tail bool) (string, bool) {
	if len(body) <= readBodyLimit {
		return body, false
	}
	if tail {
		return "…\n" + body[len(body)-readBodyLimit:], true
	}
	return body[:readBodyLimit] + "\n…", true
}

// rateLimited reports whether a gh failure is GitHub refusing for rate.
func rateLimited(err error) bool {
	detail := strings.ToLower(err.Error())
	return strings.Contains(detail, "rate limit") || strings.Contains(detail, "http 429")
}

var (
	commentFragment       = regexp.MustCompile(`^issuecomment-(\d+)$`)
	reviewCommentFragment = regexp.MustCompile(`^(?:discussion_r|r)(\d+)$`)
	reviewFragment        = regexp.MustCompile(`^pullrequestreview-(\d+)$`)
)

// githubURLPath turns a github.com or api.github.com URL inside a repository
// into that repository and the REST path under repos/OWNER/REPO that reads
// what it shows: a commit, comparison, file, pull request, issue, comment,
// review, Actions run or job, or release.
func githubURLPath(link string) (string, string, error) {
	u, err := url.Parse(link)
	if err != nil {
		return "", "", fmt.Errorf("invalid url %q: %w", link, err)
	}
	host := strings.ToLower(u.Hostname())
	segments := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	unsupported := fmt.Errorf("cannot read %s: name a GitHub commit, comparison, file, pull request, issue, comment, Actions run or release, or pass a path", link)
	if host == "api.github.com" {
		if len(segments) < 3 || segments[0] != "repos" {
			return "", "", unsupported
		}
		path := strings.Join(segments[3:], "/")
		if u.RawQuery != "" {
			path += "?" + u.RawQuery
		}
		return segments[1] + "/" + segments[2], path, nil
	}
	if host != "github.com" && host != "www.github.com" || len(segments) < 2 {
		return "", "", unsupported
	}
	repository := segments[0] + "/" + segments[1]
	rest := segments[2:]
	fragment := u.Fragment
	if match := commentFragment.FindStringSubmatch(fragment); match != nil {
		return repository, "issues/comments/" + match[1], nil
	}
	if match := reviewCommentFragment.FindStringSubmatch(fragment); match != nil && len(rest) >= 2 && rest[0] == "pull" {
		return repository, "pulls/comments/" + match[1], nil
	}
	if match := reviewFragment.FindStringSubmatch(fragment); match != nil && len(rest) >= 2 && rest[0] == "pull" {
		return repository, "pulls/" + rest[1] + "/reviews/" + match[1], nil
	}
	if len(rest) == 0 {
		return repository, "", nil
	}
	switch rest[0] {
	case "commit":
		if len(rest) >= 2 {
			return repository, "commits/" + rest[1], nil
		}
	case "commits":
		if len(rest) >= 2 {
			return repository, "commits?sha=" + strings.Join(rest[1:], "/"), nil
		}
		return repository, "commits", nil
	case "compare":
		if len(rest) >= 2 {
			return repository, "compare/" + strings.Join(rest[1:], "/"), nil
		}
	case "blob", "tree":
		if len(rest) >= 2 {
			return repository, "contents/" + strings.Join(rest[2:], "/") + "?ref=" + rest[1], nil
		}
	case "pull":
		if len(rest) >= 2 {
			if len(rest) >= 3 && (rest[2] == "files" || rest[2] == "commits") {
				return repository, "pulls/" + rest[1] + "/" + rest[2], nil
			}
			return repository, "pulls/" + rest[1], nil
		}
		return repository, "pulls", nil
	case "pulls":
		return repository, "pulls", nil
	case "issues":
		if len(rest) >= 2 {
			return repository, "issues/" + rest[1], nil
		}
		return repository, "issues", nil
	case "actions":
		switch {
		case len(rest) >= 5 && rest[1] == "runs" && rest[3] == "job":
			return repository, "actions/jobs/" + rest[4], nil
		case len(rest) >= 3 && rest[1] == "runs":
			return repository, "actions/runs/" + rest[2], nil
		case len(rest) >= 3 && rest[1] == "workflows":
			return repository, "actions/workflows/" + rest[2] + "/runs", nil
		}
		return repository, "actions/runs", nil
	case "releases":
		if len(rest) == 2 && rest[1] == "latest" {
			return repository, "releases/latest", nil
		}
		if len(rest) >= 3 && rest[1] == "tag" {
			return repository, "releases/tags/" + strings.Join(rest[2:], "/"), nil
		}
		return repository, "releases", nil
	}
	return "", "", unsupported
}

// SiteFetch is project_site_fetch: an HTTP GET of a page on a watched
// repository's own site, its homepage or GitHub Pages site, or its owner's
// github.io site, to check what is deployed.
func (m *Manager) SiteFetch(ctx context.Context, link string) (map[string]any, error) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, fmt.Errorf("invalid url %q: give an http or https URL", link)
	}
	hosts, err := m.siteHosts(ctx)
	if err != nil {
		return nil, err
	}
	if !siteHostAllowed(hosts, u.Hostname()) {
		if len(hosts) == 0 {
			return nil, fmt.Errorf("%s is not a watched project's site: no watched repository has a homepage or Pages site", u.Hostname())
		}
		return nil, fmt.Errorf("%s is not a watched project's site; only %s can be fetched", u.Hostname(), strings.Join(hosts, ", "))
	}
	client := m.siteClient
	if client == nil {
		client = http.DefaultClient
	}
	// A redirect stays on the project's sites.
	scoped := *client
	scoped.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		if !siteHostAllowed(hosts, req.URL.Hostname()) {
			return fmt.Errorf("redirected off the project's sites to %s", req.URL.Hostname())
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cache-Control", "no-cache")
	m.logf("Code Loop agent fetched %s", u.Redacted())
	resp, err := scoped.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", u.Redacted(), err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, readBodyLimit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", u.Redacted(), err)
	}
	body, truncated := limitBody(string(raw), false)
	result := map[string]any{
		"url":          resp.Request.URL.String(),
		"status":       resp.StatusCode,
		"content_type": resp.Header.Get("Content-Type"),
		"body":         body,
		"truncated":    truncated,
	}
	for _, header := range []string{"Last-Modified", "ETag", "Age", "Date"} {
		if value := resp.Header.Get(header); value != "" {
			result[strings.ToLower(strings.ReplaceAll(header, "-", "_"))] = value
		}
	}
	return result, nil
}

// siteHosts are the hosts project_site_fetch may read: each watched
// repository's homepage and GitHub Pages hosts and its owner's github.io
// host, read from GitHub and reused for siteHostsTTL.
func (m *Manager) siteHosts(ctx context.Context) ([]string, error) {
	repositories := m.watchedRepositories()
	if len(repositories) == 0 {
		return nil, errors.New("no repositories are watched")
	}
	key := strings.ToLower(strings.Join(repositories, ","))
	m.sites.mu.Lock()
	defer m.sites.mu.Unlock()
	if m.sites.key == key && time.Since(m.sites.at) < siteHostsTTL {
		return m.sites.hosts, nil
	}
	gh := m.readGH()
	var hosts []string
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if !strings.Contains(raw, "://") {
			raw = "https://" + raw
		}
		if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
			host := strings.ToLower(u.Hostname())
			if !slices.Contains(hosts, host) {
				hosts = append(hosts, host)
			}
		}
	}
	for _, repository := range repositories {
		owner, _, _ := strings.Cut(repository, "/")
		add(strings.ToLower(owner) + ".github.io")
		if output, err := gh.Run(ctx, "api", "repos/"+repository, "--jq", ".homepage // \"\""); err == nil {
			add(string(output))
		} else {
			m.logf("Code Loop agent site hosts: read %s homepage: %v", repository, err)
		}
		// A repository without Pages answers 404, which leaves only its
		// homepage.
		if output, err := gh.Run(ctx, "api", "repos/"+repository+"/pages", "--jq", "(.html_url // \"\"), (.cname // \"\")"); err == nil {
			for _, line := range strings.Split(string(output), "\n") {
				add(line)
			}
		}
	}
	m.sites.key, m.sites.hosts, m.sites.at = key, hosts, time.Now()
	return hosts, nil
}

// siteHostAllowed reports whether host is one of hosts, or its www. twin.
func siteHostAllowed(hosts []string, host string) bool {
	host = strings.ToLower(host)
	for _, allowed := range hosts {
		if host == allowed || host == "www."+allowed || "www."+host == allowed {
			return true
		}
	}
	return false
}
