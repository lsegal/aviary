import { expect, test } from "@playwright/test";
import {
	renderLinkedText,
	renderMarkdown,
	renderUserMarkdown,
} from "../src/lib/markdown";

const newTab = 'target="_blank" rel="noopener noreferrer"';

test("renderMarkdown opens an explicit Markdown link in a new tab", () => {
	expect(renderMarkdown("[text](https://example.com)")).toBe(
		`<p><a ${newTab} href="https://example.com">text</a></p>\n`,
	);
});

test("renderMarkdown opens an auto-linked bare URL in a new tab", () => {
	expect(renderMarkdown("see https://example.com.")).toBe(
		`<p>see <a ${newTab} href="https://example.com">https://example.com</a>.</p>\n`,
	);
});

test("renderMarkdown keeps link titles and raw HTML escaped", () => {
	expect(renderMarkdown('[x](https://e.com "a<b") <a href="y">z</a>')).toBe(
		`<p><a ${newTab} href="https://e.com" title="a&lt;b">x</a> &lt;a href=&quot;y&quot;&gt;z&lt;/a&gt;</p>\n`,
	);
});

test("renderMarkdown leaves in-app links in the current tab", () => {
	expect(renderMarkdown("[agent](/agents/a) [top](#top)")).toBe(
		'<p><a href="/agents/a">agent</a> <a href="#top">top</a></p>\n',
	);
});

test("renderLinkedText escapes plain text and links bare URLs in a new tab", () => {
	expect(
		renderLinkedText("<b>see</b> https://example.com/?a=1&b=2, (https://x.io)"),
	).toBe(
		`&lt;b&gt;see&lt;/b&gt; <a ${newTab} href="https://example.com/?a=1&amp;b=2">https://example.com/?a=1&amp;b=2</a>, (<a ${newTab} href="https://x.io">https://x.io</a>)`,
	);
});

test("renderUserMarkdown renders a fenced block with a language", () => {
	expect(
		renderUserMarkdown(
			'Output:\n```go\nfunc main() {\n    fmt.Println("hi")\n}\n```',
		),
	).toBe(
		'<p>Output:</p>\n<pre><code class="language-go">func main() {\n    fmt.Println(&quot;hi&quot;)\n}\n</code></pre>\n',
	);
});

test("renderUserMarkdown renders backtick and tilde fences without a language", () => {
	expect(renderUserMarkdown("```\na  b\n```\n~~~\nc\n~~~")).toBe(
		"<pre><code>a  b\n</code></pre>\n<pre><code>c\n</code></pre>\n",
	);
});

test("renderUserMarkdown runs an unclosed fence to the end of the message", () => {
	expect(renderUserMarkdown("see:\n```\nx  y\n\nz")).toBe(
		"<p>see:</p>\n<pre><code>x  y\n\nz\n</code></pre>\n",
	);
});

test("renderUserMarkdown escapes raw HTML, including inside code", () => {
	expect(
		renderUserMarkdown(
			"<b>x</b> <script>alert(1)</script>\n```\n<i>y</i>\n```",
		),
	).toBe(
		"<p>&lt;b&gt;x&lt;/b&gt; &lt;script&gt;alert(1)&lt;/script&gt;</p>\n<pre><code>&lt;i&gt;y&lt;/i&gt;\n</code></pre>\n",
	);
});

test("renderUserMarkdown keeps plain text line breaks and links", () => {
	expect(
		renderUserMarkdown("line one\nline two\n\nsee https://example.com"),
	).toBe(
		`<p>line one<br>line two</p>\n<p>see <a ${newTab} href="https://example.com">https://example.com</a></p>\n`,
	);
});

test("renderUserMarkdown renders GFM task lists, strikethrough and tables", () => {
	const html = renderUserMarkdown(
		"- [x] done\n\n~~old~~ `new`\n\n| a |\n|---|\n| 1 |",
	);
	expect(html).toContain('<input checked="" disabled="" type="checkbox"> done');
	expect(html).toContain("<del>old</del> <code>new</code>");
	expect(html).toContain("<table>");
});
