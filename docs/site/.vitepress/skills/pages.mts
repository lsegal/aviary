import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join, relative, sep } from "node:path";
import { ogHeight, ogWidth, type ShareCard, writeShareCard } from "./og.mts";
import { loadSkills, plainHeadline } from "./source.mts";

// The pages with their own share card, used by config.mts to fill in page
// titles, Open Graph and Twitter metadata, and to render and check the cards
// at build time (#763, #888). Every other page gets defaultSharePage's card.

export interface SharePage {
	// relativePath is the page's source path, as in VitePress pageData.
	relativePath: string;
	// route is the page's URL path without the site base.
	route: string;
	title: string;
	description: string;
	image: string;
	imageAlt: string;
	card: ShareCard;
}

// shareTitle is the og:title and twitter:title for a page title.
export function shareTitle(title: string): string {
	return title === "Aviary" ? title : `${title} · Aviary`;
}

// cardRoute is the address shown on a card, without the .html suffix.
function cardRoute(route: string): string {
	return `aviary.bot/${route.replace(/\.html$/, "")}`.replace(/\/$/, "");
}

function sharePage(
	relativePath: string,
	route: string,
	title: string,
	image: string,
	card: Omit<ShareCard, "route">,
): SharePage {
	return {
		relativePath,
		route,
		title,
		description: card.lede,
		image,
		imageAlt: `${card.eyebrow}: ${plainHeadline(card.headline)}`,
		card: { ...card, route: cardRoute(route) },
	};
}

// The card for pages without one of their own. Its text describes the docs
// as a whole; config.mts pairs it with each page's own title.
export const defaultSharePage = sharePage("", "", "Aviary", "og/default.png", {
	eyebrow: "Aviary docs",
	headline: "Guides and reference for the AI agent *nest.*",
	lede: "Set up Aviary, connect your channels, schedule work, and look up every command, setting and MCP tool.",
});

export function sharePages(): SharePage[] {
	const skills = loadSkills();
	return [
		sharePage("index.md", "", "Aviary", "og/home.png", {
			eyebrow: "Open source · MIT licensed",
			headline: "Aviary, the AI agent *nest.*",
			lede: "Connect your AI models to Slack, Signal, Discord, and more. Hold conversations, schedule tasks, and let your agents work for you.",
			footnote: "Single binary · CLI and web control panel",
		}),
		sharePage("projects.md", "projects.html", "Code Loop", "og/projects.png", {
			eyebrow: "Aviary · Code Loop",
			headline: "Write the issue. Aviary ships the *fix.*",
			lede: "Aviary watches your GitHub issues and puts one of your CLI providers on each one until the pull request merges.",
			tag: "aviary watch owner/repo",
		}),
		sharePage("skills/index.md", "skills/", "Skills", "og/skills.png", {
			eyebrow: "Aviary skills",
			headline: "Skills that ship *work.*",
			lede: "GitHub skills for Claude Code, Codex, Gemini CLI and other CLI agents. Install them and use them with or without Aviary.",
			tag: skills.map((skill) => skill.name).join(" · "),
		}),
		...skills.map((skill) =>
			sharePage(
				`skills/${skill.name}/index.md`,
				`skills/${skill.name}/`,
				skill.name,
				`og/skills/${skill.name}.png`,
				{
					eyebrow: skill.eyebrow,
					headline: skill.headline,
					lede: skill.tagline,
					tag: skill.name,
					footnote: "Works with or without Aviary",
				},
			),
		),
	];
}

export function findSharePage(relativePath: string): SharePage | undefined {
	return sharePages().find((page) => page.relativePath === relativePath);
}

export async function writeShareCards(outDir: string) {
	for (const page of [...sharePages(), defaultSharePage]) {
		await writeShareCard(join(outDir, page.image), page.card);
	}
}

function decode(text: string): string {
	return text
		.replaceAll("&quot;", '"')
		.replaceAll("&#39;", "'")
		.replaceAll("&lt;", "<")
		.replaceAll("&gt;", ">")
		.replaceAll("&amp;", "&");
}

function meta(html: string, key: string): string | undefined {
	const re = new RegExp(`<meta (?:property|name)="${key}" content="([^"]*)"`);
	const value = html.match(re)?.[1];
	return value === undefined ? undefined : decode(value);
}

function htmlFiles(dir: string, root = dir): string[] {
	return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
		const path = join(dir, entry.name);
		if (entry.isDirectory()) return htmlFiles(path, root);
		return entry.name.endsWith(".html") ? [relative(root, path)] : [];
	});
}

// htmlRoute maps a built file to its URL path without the base.
function htmlRoute(file: string): string {
	return file
		.split(sep)
		.join("/")
		.replace(/(^|\/)index\.html$/, "$1");
}

// pngSize reads the width and height from a PNG's IHDR chunk.
function pngSize(file: string): [number, number] {
	const header = readFileSync(file).subarray(16, 24);
	return [header.readUInt32BE(0), header.readUInt32BE(4)];
}

// checkSharePages fails the build when any built page is missing its share
// metadata, a tag describes the wrong page, or a card image is missing or
// not 1200×630. Pages with their own card must use it with their own text;
// every other page must use the default card with its own title.
export function checkSharePages(outDir: string, baseUrl: string) {
	const problems: string[] = [];
	const pages = new Map(sharePages().map((page) => [page.route, page]));
	for (const page of pages.values()) {
		const file =
			page.route === "" || page.route.endsWith("/")
				? `${page.route}index.html`
				: page.route;
		if (!existsSync(join(outDir, file))) {
			problems.push(`${page.route || "/"}: page was not built`);
		}
	}
	for (const file of htmlFiles(outDir)) {
		const route = htmlRoute(file);
		if (route === "404.html") continue;
		const name = route || "/";
		const html = readFileSync(join(outDir, file), "utf8");
		const page = pages.get(route);
		const docTitle = decode(html.match(/<title>([^<]*)<\/title>/)?.[1] ?? "");
		const want: Record<string, string> = {
			"og:url": new URL(route, baseUrl).toString(),
			"og:title": page
				? shareTitle(page.title)
				: docTitle.replace(/ \| Aviary$/, " · Aviary"),
			"og:description": page
				? page.description
				: (meta(html, "description") ?? ""),
			"og:image": new URL((page ?? defaultSharePage).image, baseUrl).toString(),
			"og:image:width": String(ogWidth),
			"og:image:height": String(ogHeight),
			"og:image:alt": (page ?? defaultSharePage).imageAlt,
			"twitter:card": "summary_large_image",
		};
		want["twitter:title"] = want["og:title"];
		want["twitter:description"] = want["og:description"];
		want["twitter:image"] = want["og:image"];
		want["twitter:image:alt"] = want["og:image:alt"];
		for (const [key, value] of Object.entries(want)) {
			const got = meta(html, key);
			if (!value) problems.push(`${name}: expected ${key} is empty`);
			else if (got !== value) {
				problems.push(`${name}: ${key} is ${got ?? "missing"}, want ${value}`);
			}
		}
	}
	for (const page of [...pages.values(), defaultSharePage]) {
		const png = join(outDir, page.image);
		if (!existsSync(png)) {
			problems.push(`${page.image} was not generated`);
			continue;
		}
		const [width, height] = pngSize(png);
		if (width !== ogWidth || height !== ogHeight) {
			problems.push(
				`${page.image} is ${width}×${height}, not ${ogWidth}×${ogHeight}`,
			);
		}
	}
	if (problems.length) {
		throw new Error(`Share cards are incomplete:\n${problems.join("\n")}`);
	}
}
