import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { Resvg } from "@resvg/resvg-js";
import satori from "satori";
import { headlineParts, repoRoot } from "./source.mts";

// Renders the 1200×630 share cards for the /skills/ pages at build time,
// in the landing pages' palette and type (#763).

export const ogWidth = 1200;
export const ogHeight = 630;

export interface ShareCard {
	eyebrow: string;
	headline: string;
	tag: string;
	footnote: string;
}

const require = createRequire(import.meta.url);

function font(pkg: string, file: string): Buffer {
	return readFileSync(
		join(dirname(require.resolve(`${pkg}/package.json`)), "files", file),
	);
}

let fonts: Parameters<typeof satori>[1]["fonts"] | undefined;

function loadFonts() {
	fonts ??= [
		{
			name: "Space Grotesk",
			data: font(
				"@fontsource/space-grotesk",
				"space-grotesk-latin-600-normal.woff",
			),
			weight: 600,
			style: "normal",
		},
		{
			name: "IBM Plex Mono",
			data: font(
				"@fontsource/ibm-plex-mono",
				"ibm-plex-mono-latin-500-normal.woff",
			),
			weight: 500,
			style: "normal",
		},
	];
	return fonts;
}

type Node = {
	type: string;
	props: Record<string, unknown> & { children?: unknown };
};

function h(
	type: string,
	style: Record<string, unknown>,
	children?: unknown,
	props: Record<string, unknown> = {},
): Node {
	return { type, props: { ...props, style, children } };
}

const palette = {
	bg: "#fbf3e6",
	paper: "#fffaf0",
	ink: "#1c120f",
	ink2: "#5d4a42",
	brand: "#b83a1d",
	brand2: "#d54d21",
	rule: "rgba(28, 18, 15, 0.12)",
};

function card(input: ShareCard, logo: string): Node {
	const parts = headlineParts(input.headline);
	return h(
		"div",
		{
			width: ogWidth,
			height: ogHeight,
			display: "flex",
			backgroundColor: palette.bg,
			backgroundImage:
				"radial-gradient(circle at 18% 12%, rgba(214, 87, 31, 0.22), transparent 45%), radial-gradient(circle at 92% 0%, rgba(198, 140, 79, 0.28), transparent 40%)",
			color: palette.ink,
			fontFamily: "Space Grotesk",
		},
		h(
			"div",
			{
				display: "flex",
				flexDirection: "column",
				justifyContent: "space-between",
				width: ogWidth - 144,
				height: ogHeight - 128,
				margin: "64px 72px",
			},
			[
				h(
					"div",
					{
						display: "flex",
						alignItems: "center",
						justifyContent: "space-between",
					},
					[
						h("div", { display: "flex", alignItems: "center", gap: 16 }, [
							h("img", { width: 52, height: 52 }, undefined, {
								src: logo,
								width: 52,
								height: 52,
							}),
							h(
								"div",
								{
									fontSize: 32,
									fontWeight: 600,
									color: palette.brand,
									letterSpacing: 1,
								},
								"Aviary",
							),
						]),
						h(
							"div",
							{
								fontFamily: "IBM Plex Mono",
								fontSize: 22,
								color: palette.ink2,
							},
							"aviary.bot/skills",
						),
					],
				),
				h("div", { display: "flex", flexDirection: "column" }, [
					h(
						"div",
						{
							fontFamily: "IBM Plex Mono",
							fontSize: 24,
							color: palette.brand,
							letterSpacing: 4,
							textTransform: "uppercase",
							marginBottom: 22,
						},
						input.eyebrow,
					),
					h(
						"div",
						{
							display: "flex",
							flexWrap: "wrap",
							fontWeight: 600,
							fontSize: 92,
							lineHeight: 1.02,
							letterSpacing: -2.3,
						},
						[
							parts.before
								? h("span", { whiteSpace: "pre" }, parts.before)
								: null,
							parts.accent
								? h(
										"span",
										{ color: palette.brand2 },
										parts.accent,
									)
								: null,
							parts.after
								? h("span", { whiteSpace: "pre" }, parts.after)
								: null,
						].filter(Boolean),
					),
				]),
				h("div", { display: "flex", alignItems: "center", gap: 20 }, [
					h(
						"div",
						{
							display: "flex",
							padding: "8px 20px",
							borderRadius: 999,
							border: "2px solid rgba(184, 58, 29, 0.4)",
							backgroundColor: "rgba(213, 77, 33, 0.12)",
							color: palette.brand,
							fontFamily: "IBM Plex Mono",
							fontSize: 26,
						},
						input.tag,
					),
					h("div", { fontSize: 24, color: palette.ink2 }, input.footnote),
				]),
			],
		),
	);
}

export async function renderShareCard(input: ShareCard): Promise<Buffer> {
	const logo = `data:image/png;base64,${readFileSync(join(repoRoot, "docs/site/public/logo.png")).toString("base64")}`;
	const svg = await satori(card(input, logo) as never, {
		width: ogWidth,
		height: ogHeight,
		fonts: loadFonts(),
	});
	return new Resvg(svg, { fitTo: { mode: "width", value: ogWidth } })
		.render()
		.asPng();
}

export async function writeShareCard(path: string, input: ShareCard) {
	mkdirSync(dirname(path), { recursive: true });
	writeFileSync(path, await renderShareCard(input));
}
