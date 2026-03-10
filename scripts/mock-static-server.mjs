import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import path from "node:path";

function parseArgs(argv) {
	const args = {};
	for (let i = 0; i < argv.length; i += 1) {
		const arg = argv[i];
		if (!arg.startsWith("--")) continue;
		const key = arg.slice(2);
		const next = argv[i + 1];
		if (!next || next.startsWith("--")) {
			args[key] = "true";
			continue;
		}
		args[key] = next;
		i += 1;
	}
	return args;
}

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
		const data = await readFile(filePath);
		res.writeHead(200).end(data);
	} catch {
		res.writeHead(404).end("not found\n");
	}
});

server.listen(port, "127.0.0.1", () => {
	console.log(`mock static server listening on http://127.0.0.1:${port}`);
});
