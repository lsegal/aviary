import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { parseArgs } from "./lib/args.mjs";

const args = parseArgs(process.argv.slice(2));
const root = path.resolve(args.root ?? ".");
const port = Number.parseInt(args.port ?? "8765", 10);

const server = createServer(async (req, res) => {
	try {
		const urlPath = new URL(req.url ?? "/", `http://${req.headers.host ?? "127.0.0.1"}`).pathname;
		const relativePath = decodeURIComponent(urlPath).replace(/^\/+/, "");
		const filePath = path.resolve(root, relativePath);
		const relative = path.relative(root, filePath);
		if (relative.startsWith("..") || path.isAbsolute(relative)) {
			res.writeHead(403).end("forbidden\n");
			return;
		}
		// <path>.redirect holds the Location a request for <path> redirects to.
		const redirect = await readFile(`${filePath}.redirect`, "utf8").catch(() => null);
		if (redirect !== null) {
			res.writeHead(302, { Location: redirect.trim() }).end();
			return;
		}
		const data = await readFile(filePath);
		res.writeHead(200).end(data);
	} catch {
		res.writeHead(404).end("not found\n");
	}
});

server.listen(port, "127.0.0.1", () => {
	console.log(`mock static server listening on http://127.0.0.1:${port}`);
});
