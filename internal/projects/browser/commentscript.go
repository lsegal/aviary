package browser

// commentsScript is the single expression evaluated in the tab per
// conversation page load. It returns the comments it could read and whether it
// recognised the page as an issue or pull request conversation at all.
//
// Extraction is anchored on the comment permalink id (`issuecomment-<id>`),
// which is the one part of a conversation page that cannot change without the
// permalinks changing with it. That id sits on the comment's *header* in
// GitHub's current markup, not on a box that also holds the body, so the
// element carrying it is walked outwards until one is found that holds a
// rendered body: an id that anchors the whole comment (which is what the older
// markup does) is read exactly the same way. Everything else is read from
// within that element, and every selector naming a class or a test id is
// written as one alternative among several, the same way the issues-list
// extractor is: GitHub renames those freely, and a single stale class name must
// not be able to blind the extractor.
//
// The handoff protocol's comments are plain one-line sentences ending in a
// `/Aviary:ID` signature, so the rendered text of a comment body carries
// everything parseClaim and mentionedIdentity read. Nothing here tries to
// recover Markdown source.
const commentsScript = `(function () {
  var idPattern = /^issuecomment-(\d+)$/;
  var bodySelector = '[data-testid="markdown-body"], [data-testid="comment-body"], .comment-body, .markdown-body, [class*="MarkdownViewer"], [class*="IssueCommentBody"]';
  // Ordered outwards from the tightest box that could hold a header and its
  // body to the timeline row the whole comment sits in.
  var containers = [
    '[data-testid^="comment-viewer-outer-box"]',
    '.react-issue-comment',
    '.js-comment-container',
    '.timeline-comment',
    '[data-timeline-event-id]'
  ];

  var nodes = document.querySelectorAll('[id^="issuecomment-"]');
  var comments = [];
  var seen = {};
  for (var i = 0; i < nodes.length; i++) {
    var node = nodes[i];
    var match = idPattern.exec(node.getAttribute('id') || '');
    if (!match || seen[match[1]]) continue;

    var container = node;
    var bodyNode = node.querySelector(bodySelector);
    for (var c = 0; !bodyNode && c < containers.length; c++) {
      var candidate = node.closest(containers[c]);
      if (!candidate) continue;
      var found = candidate.querySelector(bodySelector);
      if (found) {
        container = candidate;
        bodyNode = found;
      }
    }
    // A permalink id with no rendered body behind it is not a comment this
    // extractor can read, and reporting it with the header's own text as its
    // body would hand the handshake a comment that says nothing it wrote.
    if (!bodyNode) continue;
    var body = (bodyNode.innerText || bodyNode.textContent || '').trim();
    if (!body) continue;
    seen[match[1]] = true;

    var author = '';
    var authorNode =
      container.querySelector('a[data-testid="avatar-link"]') ||
      container.querySelector('a[data-hovercard-type="user"]') ||
      container.querySelector('a.author') ||
      container.querySelector('[class*="AuthorLink"]') ||
      container.querySelector('[class*="author"] a[href^="/"]');
    if (authorNode) {
      var href = authorNode.getAttribute('href') || '';
      var user = /^\/([^\/\s?#]+)(?:[?#].*)?$/.exec(href);
      author = user ? user[1] : (authorNode.textContent || '').trim();
    }

    var createdAt = '';
    var timeNode = container.querySelector('relative-time[datetime], time[datetime], [datetime]');
    if (timeNode) createdAt = (timeNode.getAttribute('datetime') || '').trim();

    comments.push({ id: match[1], author: author, body: body, createdAt: createdAt });
  }

  // A conversation with no comments has to be told apart from a page that has
  // not drawn yet, so the page is asked for a marker it only carries once the
  // conversation itself is rendered. Any comment found is its own evidence.
  var recognized = comments.length > 0 ||
    !!document.querySelector('[data-testid="issue-viewer-container"]') ||
    !!document.querySelector('[data-testid="issue-viewer-issue-container"]') ||
    !!document.querySelector('[data-testid="issue-viewer-comments-container"]') ||
    !!document.querySelector('[data-testid="issue-body-viewer"]') ||
    !!document.querySelector('[data-testid="issue-body"]') ||
    !!document.querySelector('[data-testid="issue-header"]') ||
    !!document.querySelector('#partial-discussion-header') ||
    !!document.querySelector('.js-discussion');

  return { comments: comments, recognized: recognized };
})()`
