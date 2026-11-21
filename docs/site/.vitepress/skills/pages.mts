import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { ogHeight, ogWidth, type ShareCard, writeShareCard } from "./og.mts";
import { loadSkills, plainHeadline } from "./source.mts";

// The /skills/ pages and their share cards, used by config.mts to fill in
// page titles, Open Graph and Twitter metadata, and to render and check the
// cards at build time (#763).

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

export function sharePages(): SharePage[] {
	const skills = loadSkills();
	const index: SharePage = {
		relativePath: "skills/index.md",
		route: "skills/",
		title: "Skills",
		description:
			"GitHub skills for Claude Code, Codex, Gemini CLI and other CLI agents. Install them and use them with or without Aviary.",
		image: "og/skills.png",
		imageAlt: "Aviary skills: Skills that ship work.",
		card: {
			eyebrow: "Aviary skills",
			headline: "Skills that ship *work.*",
			tag: skills.map((skill) => skill.name).join(" · "),
			footnote: "",
		},
	};
	return [
		index,
		...skills.map((skill) => ({
			relativePath: `skills/${skill.name}/index.md`,
			route: `skills/${skill.name}/`,
			title: skill.name,
			description: skill.tagline,
			image: `og/skills/${skill.name}.png`,
			imageAlt: `${skill.name}: ${plainHeadline(skill.headline)}`,
			card: {
				eyebrow: skill.eyebrow,
				headline: skill.headline,
				tag: skill.name,
				footnote: "Works with or without Aviary",
			},
		})),
	];
}

export function findSharePage(relativePath: string): SharePage | undefined {
	return sharePages().find((page) => page.relativePath === relativePath);
}

export async function writeShareCards(outDir: string) {
	for (const page of sharePages()) {
		await writeShareCard(join(outDir, page.image), page.card);
	}
}

function meta(html: string, key: string): string | undefined {
	const re = new RegExp(
		`<meta (?:property|name)="${key}" content="([^"]*)"`,
	);
	return html.match(re)?.[1];
}

// checkSharePages fails the build when a /skills/ page is missing its share
// metadata or its card image.
export function checkSharePages(outDir: string, baseUrl: string) {
	const problems: string[] = [];
	for (const page of sharePages()) {
		const file = join(outDir, page.route, "index.html");
		if (!existsSync(file)) {
			problems.push(`${page.route}: page was not built`);
			continue;
		}
		const html = readFileSync(file, "utf8");
		const image = meta(html, "og:image");
		if (!image?.endsWith(`/${page.image}`)) {
			problems.push(`${page.route}: og:image is ${image ?? "missing"}`);
		}
		if (meta(html, "twitter:card") !== "summary_large_image") {
			problems.push(`${page.route}: twitter:card is not summary_large_image`);
		}
		for (const key of [
			"og:title",
			"og:description",
			"og:url",
			"twitter:title",
			"twitter:description",
			"twitter:image",
		]) {
			if (!meta(html, key)) problems.push(`${page.route}: ${key} is missing`);
		}
		if (meta(html, "og:url") !== new URL(page.route, baseUrl).toString()) {
			problems.push(`${page.route}: og:url is ${meta(html, "og:url")}`);
		}
		const png = join(outDir, page.image);
		if (!existsSync(png)) {
			problems.push(`${page.route}: ${page.image} was not generated`);
			continue;
		}
		const header = readFileSync(png).subarray(16, 24);
		if (header.readUInt32BE(0) !== ogWidth || header.readUInt32BE(4) !== ogHeight) {
			problems.push(`${page.route}: ${page.image} is not ${ogWidth}×${ogHeight}`);
		}
	}
	if (problems.length) {
		throw new Error(`Skill share cards are incomplete:\n${problems.join("\n")}`);
	}
}
