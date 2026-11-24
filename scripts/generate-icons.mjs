// Generates the web app's icons from web/icons/icon.svg (#1155), rendering
// with Playwright's Chromium so every size comes from the same source:
//
//   web/public/icons/icon-<size>.png           manifest "any": the bird on an
//                                              opaque white rounded tile,
//                                              transparent only outside it
//   web/public/icons/icon-maskable-<size>.png  manifest "maskable": full-bleed
//                                              white, bird inside the safe zone
//   web/public/icons/apple-touch-icon.png      full-bleed white; iOS rounds it
//   web/public/favicon.png                     the bird alone on transparent
//
// The tab favicon stays transparent on purpose: tab strips are already a
// light or dark surface, and a white tile there would read as a white blob in
// dark mode. Installed-app surfaces (Windows taskbar, Start menu and desktop
// shortcuts, ChromeOS shelf, macOS dock) use the manifest icons, which all
// carry the tile. There are no msapplication tiles: the Windows tile meta tags
// only served legacy Edge and Internet Explorer.
//
// Usage: pnpm icons
import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "@playwright/test";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const svgPath = path.join(root, "web/icons/icon.svg");
const publicDir = path.join(root, "web/public");

// Mark width as a share of the canvas, and tile corner radius as a share of
// the canvas (0 is full-bleed). The maskable mark is smaller so its whole
// bounding box fits the 80% safe-zone circle.
const variants = {
	any: { markWidth: 0.65, radius: 0.22, tile: true },
	maskable: { markWidth: 0.6, radius: 0, tile: true },
	apple: { markWidth: 0.65, radius: 0, tile: true },
	favicon: { markWidth: 0.94, radius: 0, tile: false },
};

const outputs = [
	...[48, 72, 96, 144, 192, 256, 512].map((size) => ({
		file: `icons/icon-${size}.png`,
		size,
		variant: "any",
	})),
	...[192, 512].map((size) => ({
		file: `icons/icon-maskable-${size}.png`,
		size,
		variant: "maskable",
	})),
	{ file: "icons/apple-touch-icon.png", size: 180, variant: "apple" },
	{ file: "favicon.png", size: 64, variant: "favicon" },
];

const svg = await readFile(svgPath, "utf8");
const markHref = svg.match(/<image[^>]*\bhref="([^"]+)"/)?.[1];
if (!markHref) throw new Error(`${svgPath}: no <image href> for the mark`);
const mark = await readFile(path.resolve(path.dirname(svgPath), markHref));
const markDataURL = `data:image/png;base64,${mark.toString("base64")}`;

const browser = await chromium.launch();
try {
	const page = await browser.newPage();
	for (const { file, size, variant } of outputs) {
		const png = await page.evaluate(
			async ({ svg, markDataURL, size, layout }) => {
				const load = (src) =>
					new Promise((resolve, reject) => {
						const img = new Image();
						img.onload = () => resolve(img);
						img.onerror = () => reject(new Error(`cannot load ${src}`));
						img.src = src;
					});
				const canvas = (n) => {
					const c = document.createElement("canvas");
					c.width = n;
					c.height = n;
					return c;
				};

				// Find the visible bird inside logo.png so the layout can center
				// and size the bird itself rather than the padded logo canvas.
				const logo = await load(markDataURL);
				const probe = canvas(logo.naturalWidth).getContext("2d");
				probe.drawImage(logo, 0, 0);
				const { data, width } = probe.getImageData(
					0,
					0,
					logo.naturalWidth,
					logo.naturalHeight,
				);
				let [x0, y0, x1, y1] = [width, width, 0, 0];
				for (let i = 3; i < data.length; i += 4) {
					if (data[i] < 8) continue;
					const x = ((i - 3) / 4) % width;
					const y = Math.floor((i - 3) / 4 / width);
					x0 = Math.min(x0, x);
					y0 = Math.min(y0, y);
					x1 = Math.max(x1, x);
					y1 = Math.max(y1, y);
				}

				const doc = new DOMParser().parseFromString(svg, "image/svg+xml");
				const view = doc.documentElement.viewBox.baseVal.width;
				const tile = doc.getElementById("tile");
				const image = doc.getElementById("mark");
				if (layout.tile) {
					tile.setAttribute("rx", String(layout.radius * view));
				} else {
					tile.remove();
				}
				const scale = (layout.markWidth * view) / (x1 - x0 + 1);
				const logoSize = logo.naturalWidth * scale;
				image.setAttribute("href", markDataURL);
				image.setAttribute("width", String(logoSize));
				image.setAttribute("height", String(logoSize));
				image.setAttribute("x", String(view / 2 - ((x0 + x1 + 1) / 2) * scale));
				image.setAttribute("y", String(view / 2 - ((y0 + y1 + 1) / 2) * scale));

				// Rasterize at full size, then downscale with high-quality
				// smoothing so small sizes stay crisp.
				const source = new XMLSerializer().serializeToString(doc);
				const rendered = await load(
					`data:image/svg+xml;base64,${btoa(unescape(encodeURIComponent(source)))}`,
				);
				const full = canvas(view);
				full.getContext("2d").drawImage(rendered, 0, 0, view, view);
				const out = canvas(size);
				const ctx = out.getContext("2d");
				ctx.imageSmoothingQuality = "high";
				ctx.drawImage(full, 0, 0, size, size);
				return out.toDataURL("image/png").split(",")[1];
			},
			{ svg, markDataURL, size, layout: variants[variant] },
		);
		await writeFile(path.join(publicDir, file), Buffer.from(png, "base64"));
		console.log(`wrote web/public/${file} (${size}x${size}, ${variant})`);
	}
} finally {
	await browser.close();
}
