import { marked, type Tokens } from "marked";

const markdownRenderer = new marked.Renderer();

markdownRenderer.html = (token) => escapeHTML(token.text);

/** Opens external links in a new tab so they never navigate away from Aviary. */
markdownRenderer.link = function (token: Tokens.Link) {
	const html = marked.Renderer.prototype.link.call(this, token);
	return isExternalURL(token.href) && html.startsWith("<a ")
		? `<a target="_blank" rel="noopener noreferrer" ${html.slice(3)}`
		: html;
};

const externalURLPattern = /^(?:[a-z][a-z\d+.-]*:|\/\/)/i;

/** Reports whether href leaves the app, as opposed to an in-app path or anchor. */
export function isExternalURL(href: string): boolean {
	return externalURLPattern.test(href.trim());
}

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

/**
 * Renders an untrusted user chat message as Markdown. Single newlines stay
 * line breaks, as typed, so plain multi-line messages read as before.
 */
export function renderUserMarkdown(text: string): string {
	return marked.parse(text, {
		async: false,
		breaks: true,
		renderer: markdownRenderer,
	}) as string;
}

const bareURLPattern = /https?:\/\/[^\s<>"']*[^\s<>"'.,;:!?)\]}]/g;

/**
 * Renders untrusted plain text to HTML, escaping everything and turning bare
 * URLs into links through the same renderer as Markdown.
 */
export function renderLinkedText(text: string): string {
	let html = "";
	let last = 0;
	for (const match of text.matchAll(bareURLPattern)) {
		const href = match[0];
		html += escapeHTML(text.slice(last, match.index));
		html += markdownRenderer.link({
			type: "link",
			raw: href,
			href,
			title: null,
			text: href,
			tokens: [],
			autolink: true,
		});
		last = match.index + href.length;
	}
	return html + escapeHTML(text.slice(last));
}
