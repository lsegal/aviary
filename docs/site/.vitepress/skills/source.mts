import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { parse } from "yaml";

// Reads the skills Aviary ships straight from their sources, so the landing
// pages under /skills/ never drift from the skills themselves (#763).

export const repoRoot = resolve(
	dirname(fileURLToPath(import.meta.url)),
	"../../../..",
);
export const skillsRoot = join(repoRoot, ".agents/skills");
export const repoUrl = "https://github.com/lsegal/aviary";

export interface SkillStep {
	title: string;
	text: string;
}

export interface SkillOption {
	name: string;
	text: string;
}

export interface HeadlineParts {
	before: string;
	accent: string;
	after: string;
}

export interface Skill {
	name: string;
	displayName: string;
	shortDescription: string;
	description: string;
	eyebrow: string;
	headline: string;
	headlineParts: HeadlineParts;
	tagline: string;
	usage: string;
	steps: SkillStep[];
	aviary: string;
	requirements: string[];
	options: SkillOption[];
	sourceUrl: string;
	skillUrl: string;
	docsPath: string;
}

// skillNames reads CodingSkillNames from codingskills.go, the list of skills
// Aviary installs for providers, in display order.
export function skillNames(): string[] {
	const go = readFileSync(join(repoRoot, "codingskills.go"), "utf8");
	const match = go.match(/CodingSkillNames\s*=\s*\[\]string\{([^}]*)\}/);
	if (!match) throw new Error("CodingSkillNames not found in codingskills.go");
	return [...match[1].matchAll(/"([^"]+)"/g)].map((m) => m[1]);
}

function frontmatter(markdown: string): Record<string, string> {
	const match = markdown.match(/^---\n([\s\S]*?)\n---/);
	return match ? parse(match[1]) : {};
}

function required<T>(value: T | undefined, name: string, field: string): T {
	if (value === undefined || value === null || value === "") {
		throw new Error(`.agents/skills/${name}: missing ${field}`);
	}
	return value;
}

export function loadSkill(name: string): Skill {
	const dir = join(skillsRoot, name);
	const meta = frontmatter(readFileSync(join(dir, "SKILL.md"), "utf8"));
	const openai = parse(readFileSync(join(dir, "agents/openai.yaml"), "utf8"));
	const landing = parse(readFileSync(join(dir, "landing.yaml"), "utf8"));
	return {
		name,
		displayName: required(openai?.interface?.display_name, name, "display_name"),
		shortDescription: required(
			openai?.interface?.short_description,
			name,
			"short_description",
		),
		description: required(meta.description, name, "description"),
		eyebrow: required(landing.eyebrow, name, "eyebrow"),
		headline: required(landing.headline, name, "headline"),
		headlineParts: headlineParts(landing.headline),
		tagline: required(landing.tagline, name, "tagline"),
		usage: required(landing.usage, name, "usage"),
		steps: required(landing.steps, name, "steps"),
		aviary: required(landing.aviary, name, "aviary"),
		requirements: required(landing.requirements, name, "requirements"),
		options: landing.options ?? [],
		sourceUrl: `${repoUrl}/tree/main/.agents/skills/${name}`,
		skillUrl: `${repoUrl}/blob/main/.agents/skills/${name}/SKILL.md`,
		docsPath: `/guide/skills/${name}`,
	};
}

export function loadSkills(): Skill[] {
	return skillNames().map(loadSkill);
}

// headlineParts splits a headline on its *accent* word.
export function headlineParts(headline: string): HeadlineParts {
	const match = headline.match(/^(.*?)\*([^*]+)\*(.*)$/);
	if (!match) return { before: headline, accent: "", after: "" };
	return { before: match[1], accent: match[2], after: match[3] };
}

// plainHeadline drops the accent markers for titles and alt text.
export function plainHeadline(headline: string): string {
	return headline.replaceAll("*", "");
}
