import { defineConfig } from "vitepress";

const base = process.env.DOCS_BASE ?? "/";

export default defineConfig({
	base,
	title: "Aviary",
	description:
		"Aviary is the control plane for long-running AI agents, scheduled work, operator tooling, and channel-connected assistants.",
	head: [
		["link", { rel: "icon", type: "image/png", href: `${base}logo.png` }],
	],
	themeConfig: {
		logo: "/logo.png",
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
				"Draft docs scaffold for the Aviary marketing site and product reference.",
			copyright: "Copyright © Aviary contributors",
		},
	},
});
