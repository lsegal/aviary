import { expect, test } from "@playwright/test";
import { renderLinkedText, renderMarkdown } from "../src/lib/markdown";

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
