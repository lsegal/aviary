import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { Resvg } from "@resvg/resvg-js";
import satori from "satori";
import { headlineParts, repoRoot } from "./source.mts";

// Renders the 1200×630 share cards at build time (#763), in the site's
// current palette and type from theme/custom.css (#780, #888).

export const ogWidth = 1200;
export const ogHeight = 630;

export interface ShareCard {
	// route is the page address shown in the card's top right.
	route: string;
	eyebrow: string;
	// headline wraps its accent word in *asterisks*, as in landing.yaml.
	headline: string;
	lede: string;
	tag?: string;
	footnote?: string;
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

// The light theme's variables in theme/custom.css.
const palette = {
	bg: "#ffffff",
	bgAlt: "#fafafa",
	ink: "#1d1a19",
	ink2: "#57524f",
	brand: "#b83a1d",
	brand2: "#d54d21",
	brand3: "#7f1809",
	brandSoft: "rgba(213, 77, 33, 0.12)",
	ember: "#e06430",
};

// Everything sits inside these margins, so nothing reaches the card's edge.
const marginX = 72;
const marginY = 56;
const contentWidth = ogWidth - 2 * marginX;

// The headline shrinks from the page's 80px title size until it fits in
// headlineHeight; three lines at the smallest size still fit.
const headlineSizes = [80, 76, 72, 68, 64, 60, 56, 52];
const headlineLineHeight = 1.04;
const headlineHeight = 176;

function headlineNode(headline: string, size: number): Node {
	const parts = headlineParts(headline);
	// Each word is its own flex item so long headlines wrap instead of
	// running off the card.
	const words = [
		...parts.before.split(/\s+/).map((text) => ({ text, accent: false })),
		...parts.accent.split(/\s+/).map((text) => ({ text, accent: true })),
		...parts.after.split(/\s+/).map((text) => ({ text, accent: false })),
	].filter((word) => word.text);
	return h(
		"div",
		{
			display: "flex",
			flexWrap: "wrap",
			width: contentWidth,
			columnGap: size * 0.26,
			fontWeight: 600,
			fontSize: size,
			lineHeight: headlineLineHeight,
			letterSpacing: size * -0.025,
		},
		words.map((word) =>
			word.accent
				? h(
						"span",
						{
							backgroundImage: `linear-gradient(135deg, ${palette.brand2}, ${palette.brand3} 60%, ${palette.ember})`,
							backgroundClip: "text",
							color: "transparent",
						},
						word.text,
					)
				: h("span", {}, word.text),
		),
	);
}

async function headlineBox(headline: string, size: number) {
	const svg = await satori(headlineNode(headline, size) as never, {
		width: contentWidth,
		height: 1000,
		fonts: loadFonts(),
	});
	return new Resvg(svg).getBBox();
}

// fitHeadline picks the largest headline size whose wrapped text fits the
// headline box, measured from the rendered glyphs' bounds.
export async function fitHeadline(headline: string): Promise<number> {
	for (const size of headlineSizes) {
		const box = await headlineBox(headline, size);
		if (!box) return size;
		if (
			box.x + box.width <= contentWidth &&
			box.y + box.height <= headlineHeight
		) {
			return size;
		}
	}
	throw new Error(`share card headline does not fit: ${headline}`);
}

const ellipsis = {
	overflow: "hidden",
	textOverflow: "ellipsis",
	whiteSpace: "nowrap",
};

function card(input: ShareCard, logo: string, headlineSize: number): Node {
	return h(
		"div",
		{
			width: ogWidth,
			height: ogHeight,
			display: "flex",
			flexDirection: "column",
			backgroundColor: palette.bg,
			backgroundImage: `radial-gradient(circle at 20% -10%, rgba(214, 87, 31, 0.1), transparent 55%), radial-gradient(circle at 100% 10%, rgba(184, 58, 29, 0.06), transparent 50%), linear-gradient(180deg, ${palette.bg}, ${palette.bgAlt})`,
			color: palette.ink,
			fontFamily: "Space Grotesk",
		},
		[
			h(
				"div",
				{
					display: "flex",
					flexDirection: "column",
					justifyContent: "space-between",
					width: contentWidth,
					height: ogHeight - 2 * marginY - 8,
					margin: `${marginY}px ${marginX}px`,
					marginBottom: marginY - 8,
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
							h("div", { display: "flex", alignItems: "center", gap: 14 }, [
								h("img", { width: 48, height: 48 }, undefined, {
									src: logo,
									width: 48,
									height: 48,
								}),
								h(
									"div",
									{ fontSize: 30, fontWeight: 600, color: palette.ink },
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
								input.route,
							),
						],
					),
					h("div", { display: "flex", flexDirection: "column" }, [
						h(
							"div",
							{
								fontFamily: "IBM Plex Mono",
								fontSize: 22,
								color: palette.brand,
								letterSpacing: 3,
								textTransform: "uppercase",
								marginBottom: 18,
								...ellipsis,
							},
							input.eyebrow,
						),
						headlineNode(input.headline, headlineSize),
						h(
							"div",
							{
								display: "block",
								marginTop: 18,
								fontSize: 27,
								fontWeight: 600,
								lineHeight: 1.35,
								color: palette.ink2,
								lineClamp: 2,
							},
							input.lede,
						),
					]),
					h(
						"div",
						{ display: "flex", alignItems: "center", gap: 20, height: 44 },
						[
							input.tag
								? h(
										"div",
										{
											display: "flex",
											flexShrink: 0,
											padding: "6px 18px",
											borderRadius: 999,
											border: "2px solid rgba(184, 58, 29, 0.4)",
											backgroundColor: palette.brandSoft,
											color: palette.brand,
											fontFamily: "IBM Plex Mono",
											fontSize: 24,
										},
										input.tag,
									)
								: null,
							input.footnote
								? h(
										"div",
										{ fontSize: 24, color: palette.ink2, ...ellipsis },
										input.footnote,
									)
								: null,
						].filter(Boolean),
					),
				],
			),
			// The brand bar along the bottom edge, as on the static logo card.
			h("div", {
				width: ogWidth,
				height: 8,
				backgroundImage: `linear-gradient(90deg, ${palette.brand3}, ${palette.brand}, ${palette.ember})`,
			}),
		],
	);
}

export async function renderShareCard(input: ShareCard): Promise<Buffer> {
	const logo = `data:image/png;base64,${readFileSync(join(repoRoot, "docs/site/public/logo.png")).toString("base64")}`;
	const size = await fitHeadline(input.headline);
	const svg = await satori(card(input, logo, size) as never, {
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
