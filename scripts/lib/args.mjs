/**
 * Parses `--key value` pairs from a script's arguments. A flag with no value
 * (followed by another flag or by nothing) is "true"; arguments that aren't
 * flags are ignored.
 */
export function parseArgs(argv) {
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
