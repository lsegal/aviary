import { defineConfig } from "vitepress";

const base = process.env.DOCS_BASE ?? "/";
const siteUrl = process.env.DOCS_SITE_URL ?? "https://aviary.bot";
const logoPath = `${base}logo.png`;
const socialImagePath = `${base}logo-social.png`;
const pageUrl = new URL(base, siteUrl).toString();
const imageUrl = new URL(socialImagePath, siteUrl).toString();
const title = "Aviary";
const description =
	"Aviary is a full AI assistant platform. Connect your AI models to Slack, Signal, Discord, etc., have conversations, set up scheduled tasks, and let your agents work for you. All managed from a CLI or a web-based control panel.";

export default defineConfig({
	base,
	title,
	description,
	transformHead() {
		return [
			["link", { rel: "icon", type: "image/png", href: logoPath }],
			["link", { rel: "canonical", href: pageUrl }],
			["meta", { property: "og:type", content: "website" }],
			["meta", { property: "og:title", content: title }],
			["meta", { property: "og:description", content: description }],
			["meta", { property: "og:url", content: pageUrl }],
			["meta", { property: "og:image", content: imageUrl }],
			["meta", { property: "og:image:width", content: "512" }],
			["meta", { property: "og:image:height", content: "512" }],
			["meta", { property: "og:image:alt", content: "Aviary logo on white background" }],
			["meta", { name: "twitter:card", content: "summary_large_image" }],
			["meta", { name: "twitter:title", content: title }],
			["meta", { name: "twitter:description", content: description }],
			["meta", { name: "twitter:image", content: imageUrl }],
			["meta", { name: "twitter:image:alt", content: "Aviary logo on white background" }],
		];
	},
	themeConfig: {
		logo: logoPath,
		nav: [
			{ text: "Guide", link: "/guide/" },
			{ text: "Reference", link: "/reference/" },
			{ text: "GitHub", link: "https://github.com/lsegal/aviary" },
		],
		sidebar: {
			"/guide/": [
				{
					text: "Guide",
					items: [
						{ text: "Overview", link: "/guide/" },
						{ text: "Getting Started", link: "/guide/getting-started" },
						{ text: "Control Panel", link: "/guide/control-panel" },
						{ text: "Configuration", link: "/guide/configuration" },
						{ text: "Operations", link: "/guide/operations" },
					],
				},
			],
			"/reference/": [
				{
					text: "Reference",
					items: [
						{ text: "Index", link: "/reference/" },
						{ text: "UI Surface", link: "/reference/ui/control-panel" },
						{ text: "MCP Index", link: "/reference/mcp/" },
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
						{ text: "Memory Tools", link: "/reference/mcp/memory" },
						{ text: "Auth Tools", link: "/reference/mcp/auth" },
						{
							text: "Server And Config Tools",
							link: "/reference/mcp/server-and-config",
						},
						{
							text: "Usage And Skills Tools",
							link: "/reference/mcp/usage-and-skills",
						},
					],
				},
			],
		},
		socialLinks: [
			{ icon: "github", link: "https://github.com/lsegal/aviary" },
		],
		search: {
			provider: "local",
		},
		footer: {
			message:
				'Licensed under the MIT License. <a href="https://github.com/lsegal/aviary" target="_blank" rel="noreferrer">GitHub</a>',
			copyright: "Copyright © 2026 Loren Segal",
		},
	},
});
