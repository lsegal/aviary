package projects

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/projects/core"
)

// GitHub's upload limits for issue and comment attachments.
const (
	maxImageAttachmentBytes = 10 << 20
	maxVideoAttachmentBytes = 100 << 20
)

// maxAttachments is the most files gh takes in one command.
const maxAttachments = 50

// githubVideoTypes are the video types GitHub accepts as attachments.
var githubVideoTypes = map[string]bool{"video/mp4": true, "video/quicktime": true, "video/webm": true}

// githubImageTypes are the image types GitHub accepts as attachments.
var githubImageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/svg+xml": true}

// checkAttachments returns the absolute paths of files, deduplicated, or an
// error naming every file GitHub would not take and why, so a filing never
// drops one silently.
func checkAttachments(files []string) ([]string, error) {
	var paths, problems []string
	seen := map[string]bool{}
	for _, file := range files {
		path, err := checkAttachment(file)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	if len(paths) > maxAttachments {
		problems = append(problems, fmt.Sprintf("%d attachments is more than the %d GitHub takes at once", len(paths), maxAttachments))
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("cannot attach %s", strings.Join(problems, "; "))
	}
	return paths, nil
}

// checkAttachment returns file's absolute path when it is an image or video
// GitHub accepts, within its size limit.
func checkAttachment(file string) (string, error) {
	path, err := filepath.Abs(strings.TrimSpace(file))
	if err != nil || strings.TrimSpace(file) == "" {
		return "", fmt.Errorf("%q: not a file path", file)
	}
	info, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		return "", fmt.Errorf("%s: file does not exist", path)
	case err != nil:
		return "", fmt.Errorf("%s: %v", path, err)
	case !info.Mode().IsRegular():
		return "", fmt.Errorf("%s: not a regular file", path)
	}
	contentType := fileContentType(path)
	limit := int64(maxImageAttachmentBytes)
	switch {
	case githubImageTypes[contentType]:
	case githubVideoTypes[contentType]:
		limit = maxVideoAttachmentBytes
	default:
		if contentType == "" {
			contentType = "unknown type"
		}
		return "", fmt.Errorf("%s: unsupported type %s; GitHub takes only images (PNG, JPEG, GIF, WebP, SVG) and videos (MP4, MOV, WebM)", path, contentType)
	}
	if info.Size() > limit {
		return "", fmt.Errorf("%s: too large (%d MB; GitHub takes up to %d MB for %s)", path, (info.Size()+(1<<20)-1)>>20, limit>>20, contentType)
	}
	return path, nil
}

// fileContentType is path's MIME type from its extension, or sniffed from its
// first bytes.
func fileContentType(path string) string {
	if contentType := llm.ContentTypeFromName(path); contentType != "" {
		return contentType
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close() //nolint:errcheck
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	contentType, _, _ := strings.Cut(http.DetectContentType(head[:n]), ";")
	return contentType
}

// GHCLI posts comments with attachments; browser mode passes them through.
var _ core.AttachmentCommentPoster = GHCLI{}

// PostCommentWithAttachments posts body on repo#number with files uploaded
// and embedded through gh's --attach, and reports the comment's URL.
func (g GHCLI) PostCommentWithAttachments(ctx context.Context, repo string, number int, body string, files []string) (string, error) {
	args := []string{"issue", "comment", strconv.Itoa(number), "--repo", repo, "--body", body}
	for _, file := range files {
		args = append(args, "--attach", file)
	}
	output, err := g.run(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("post comment with attachments on %s#%d: %w: %s", repo, number, err, strings.TrimSpace(string(output)))
	}
	return lastLine(strings.TrimSpace(string(output))), nil
}
