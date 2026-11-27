// Duplicating a scheduled task (#1465): the copy's name, and the source
// repository's mentions in its prompt when it is copied to another project.
// The Go side mirrors both in internal/mcp/task_duplicate.go for
// project_task_duplicate.

const SUFFIX = /^(.*)-(\d+)$/;

function escapeRegExp(s: string): string {
	return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * The name a copy of the task named source gets among the destination's
 * task names taken: source itself when it is free there, else source with
 * the next free numeric suffix. A source that already ends in -N counts up
 * from the highest index its base has taken, so x and x-2 both copy to x-3.
 */
export function nextTaskName(source: string, taken: Iterable<string>): string {
	const names = new Set(taken);
	if (!names.has(source)) return source;
	const match = SUFFIX.exec(source);
	const base = match?.[1] ? match[1] : source;
	let highest = match?.[1] ? Number(match[2]) : 1;
	const sibling = new RegExp(`^${escapeRegExp(base)}-(\\d+)$`);
	for (const name of names) {
		const m = sibling.exec(name);
		if (m) highest = Math.max(highest, Number(m[1]));
	}
	let next = highest + 1;
	while (names.has(`${base}-${next}`)) next++;
	return `${base}-${next}`;
}

/** A path-like token: absolute, home-relative or relative with a slash. */
const PATH_TOKEN = /(?:~|\.{1,2})?(?:\/\.{0,2}[\w@+-]+(?:\.[\w@+-]+)*)+\/?/g;

/** The repository's owner/repo name, case-insensitive, as a whole word. */
function slugPattern(repo: string): RegExp {
	return new RegExp(
		`(?<![\\w.-])${escapeRegExp(repo)}(?![\\w-])(?!\\.(?!git\\b)\\w)`,
		"gi",
	);
}

/** The repository's bare name, as a directory in a local checkout path. */
function repoName(repo: string): string {
	return repo.split("/").pop() ?? "";
}

/**
 * The mentions of repository source (owner/repo) in prompt: its owner/repo
 * name, which GitHub URLs contain, and local checkout paths ending in, or
 * running through, a directory named after it.
 */
export function repoReferences(prompt: string, source: string): string[] {
	if (!source.includes("/")) return [];
	const found: string[] = [];
	for (const m of prompt.matchAll(slugPattern(source))) found.push(m[0]);
	const name = repoName(source);
	const withoutSlug = prompt.replace(slugPattern(source), " ");
	for (const m of withoutSlug.matchAll(PATH_TOKEN)) {
		if (m[0].split("/").includes(name)) found.push(m[0]);
	}
	return found;
}

/**
 * The prompt with repository source's mentions, as repoReferences finds
 * them, replaced with repository dest.
 */
export function replaceRepoReferences(
	prompt: string,
	source: string,
	dest: string,
): string {
	if (!source.includes("/") || !dest.includes("/")) return prompt;
	const placeholder = "\u0000repo\u0000";
	const from = repoName(source);
	const to = repoName(dest);
	return prompt
		.replace(slugPattern(source), placeholder)
		.replace(PATH_TOKEN, (token) =>
			token
				.split("/")
				.map((part) => (part === from ? to : part))
				.join("/"),
		)
		.replaceAll(placeholder, dest);
}
