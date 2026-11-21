import { defineConfig, type HeadConfig } from "vitepress";
import {
	checkSharePages,
	findSharePage,
	writeShareCards,
} from "./skills/pages.mts";

const base = process.env.DOCS_BASE ?? "/";
const siteUrl = process.env.DOCS_SITE_URL ?? "https://aviary.bot";
const logoPath = `${base}logo.png`;
const baseUrl = new URL(base, siteUrl).toString();
const title = "Aviary";
const description =
	"Aviary is a full AI assistant platform. Connect your AI models to Slack, Signal, Discord, etc., have conversations, set up scheduled tasks, and let your agents work for you. All managed from a CLI or a web-based control panel.";

// pageRoute maps a page source path to its URL path without the base.
function pageRoute(relativePath: string): string {
	return relativePath
		.replace(/(^|\/)index\.md$/, "$1")
		.replace(/\.md$/, ".html");
}

export default defineConfig({
	base,
	title,
	description,
	transformPageData(pageData) {
		const page = findSharePage(pageData.relativePath);
		if (!page) return;
		pageData.title = page.title;
		pageData.description = page.description;
	},
	transformHead({ pageData }) {
		const page = findSharePage(pageData.relativePath);
		const pageUrl = new URL(pageRoute(pageData.relativePath), baseUrl).toString();
		// Skill pages get their own generated 1200×630 card (#763).
		const share = page
			? {
					title: `${page.title} · ${title}`,
					description: page.description,
					image: new URL(page.image, baseUrl).toString(),
					width: "1200",
					height: "630",
					alt: page.imageAlt,
				}
			: {
					title,
					description,
					image: new URL("logo-social.png", baseUrl).toString(),
					width: "512",
					height: "512",
					alt: "Aviary logo on white background",
				};
		const head: HeadConfig[] = [
			["link", { rel: "icon", type: "image/png", href: logoPath }],
			["link", { rel: "canonical", href: pageUrl }],
			["meta", { property: "og:type", content: "website" }],
			["meta", { property: "og:site_name", content: title }],
			["meta", { property: "og:title", content: share.title }],
			["meta", { property: "og:description", content: share.description }],
			["meta", { property: "og:url", content: pageUrl }],
			["meta", { property: "og:image", content: share.image }],
			["meta", { property: "og:image:width", content: share.width }],
			["meta", { property: "og:image:height", content: share.height }],
			["meta", { property: "og:image:alt", content: share.alt }],
			["meta", { name: "twitter:card", content: "summary_large_image" }],
			["meta", { name: "twitter:title", content: share.title }],
			["meta", { name: "twitter:description", content: share.description }],
			["meta", { name: "twitter:image", content: share.image }],
			["meta", { name: "twitter:image:alt", content: share.alt }],
		];
		return head;
	},
	async buildEnd(siteConfig) {
		await writeShareCards(siteConfig.outDir);
		checkSharePages(siteConfig.outDir, baseUrl);
	},
	themeConfig: {
		logo: logoPath,
		nav: [
			{ text: "Guide", link: "/guide/" },
			{ text: "Skills", link: "/skills/" },
			{ text: "Reference", link: "/reference/" },
		],
		sidebar: {
			"/guide/": [
				{
					text: "Guide",
					items: [
						{ text: "Overview", link: "/guide/" },
						{ text: "Getting Started", link: "/guide/getting-started" },
						{ text: "Configuration", link: "/guide/configuration" },
						{ text: "Channels", link: "/guide/channels" },
						{
							text: "Security & Permissions",
							link: "/guide/security-permissions",
						},
						{
							text: "Sessions & Conversations",
							link: "/guide/sessions-conversations",
						},
						{ text: "Scheduled Tasks", link: "/guide/scheduled-tasks" },
						{ text: "Projects", link: "/guide/projects" },
						{ text: "The Code Loop on GitHub", link: "/guide/code-loop" },
						{ text: "Control Panel", link: "/guide/control-panel" },
						{ text: "Operations", link: "/guide/operations" },
					],
				},
				{
					text: "Skills",
					items: [
						{ text: "Overview", link: "/guide/skills/" },
						{ text: "All skills", link: "/skills/" },
						{ text: "gh-fix", link: "/guide/skills/gh-fix" },
						{ text: "gh-discuss", link: "/guide/skills/gh-discuss" },
						{ text: "gh-safe-merge", link: "/guide/skills/gh-safe-merge" },
						{ text: "gh-create", link: "/guide/skills/gh-create" },
					],
				},
			],
			"/reference/": [
				{
					text: "Reference",
					items: [
						{ text: "Index", link: "/reference/" },
						{ text: "CLI Reference", link: "/reference/cli" },
						{ text: "Configuration Reference", link: "/reference/config" },
						{
							text: "CLI Provider Definitions",
							link: "/reference/cli-definitions",
						},
						{ text: "UI Surface", link: "/reference/ui/control-panel" },
					],
				},
				{
					text: "MCP Tools",
					items: [
						{ text: "Overview", link: "/reference/mcp/" },
						{ text: "Agent Tools", link: "/reference/mcp/agents" },
						{ text: "Session Tools", link: "/reference/mcp/sessions" },
						{
							text: "Task And Job Tools",
							link: "/reference/mcp/tasks-and-jobs",
						},
						{
							text: "Browser And Channel Tools",
							link: "/reference/mcp/browser-and-channels",
						},
						{
							text: "Files And Notes Tools",
							link: "/reference/mcp/files-and-notes",
						},
						{ text: "Auth Tools", link: "/reference/mcp/auth" },
						{ text: "Projects Tools", link: "/reference/mcp/projects" },
						{
							text: "Server And Config Tools",
							link: "/reference/mcp/server-and-config",
						},
						{
							text: "Projects Tools",
							link: "/reference/mcp/projects",
						},
						{
							text: "Usage And Skills Tools",
							link: "/reference/mcp/usage-and-skills",
						},
					],
				},
			],
		},
		socialLinks: [{ icon: "github", link: "https://github.com/lsegal/aviary" }],
		search: {
			provider: "local",
		},
		footer: {
			message:
				'Licensed under the MIT License. Project on <a href="https://github.com/lsegal/aviary" target="_blank" rel="noreferrer">GitHub</a>.',
			copyright: "Copyright © 2026 Loren Segal",
		},
	},
});
