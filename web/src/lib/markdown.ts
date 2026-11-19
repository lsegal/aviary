import { marked } from "marked";

const markdownRenderer = new marked.Renderer();

markdownRenderer.html = (token) => escapeHTML(token.text);

export function escapeHTML(text: string): string {
	return text
		.replaceAll("&", "&amp;")
		.replaceAll("<", "&lt;")
		.replaceAll(">", "&gt;")
		.replaceAll('"', "&quot;")
		.replaceAll("'", "&#39;");
}

/** Renders untrusted Markdown to HTML with raw HTML escaped. */
export function renderMarkdown(text: string): string {
	return marked.parse(text, {
		async: false,
		renderer: markdownRenderer,
	}) as string;
}
