// Providers reached through a CLI, as core Aviary's provider_cli_list reports them: the
// agent doctor's view of every CLI definition. Settings → Providers
// renders them, and Projects reads the same list to choose a job's CLI.

/** The doctor's overall verdict: ready, installed but needing attention, or not installed. */
export type CLIStatus = "ok" | "warn" | "missing";

export interface CLIProvider {
	name: string;
	installed: boolean;
	status: CLIStatus;
	/** The executable the definition names, and where it was found. */
	binary: string;
	path: string;
	version: string;
	versionNote: string;
	signedIn: boolean;
	/** "signed in", "signed out" or "unknown". */
	auth: string;
	quota: string;
	tracksQuota: boolean;
	quotaError: string;
	/** Fully qualified model specs, such as claude/opus. */
	models: string[];
	modelNote: string;
	defaultModel: string;
	projectsNote: string;
}

type Raw = Record<string, unknown>;

function obj(v: unknown): Raw {
	return v && typeof v === "object" && !Array.isArray(v) ? (v as Raw) : {};
}

function str(v: unknown): string {
	return typeof v === "string" ? v : "";
}

function strings(v: unknown): string[] {
	return Array.isArray(v)
		? v.filter((s): s is string => typeof s === "string" && !!s.trim())
		: [];
}

export function normalizeCLIList(raw: unknown): CLIProvider[] {
	const r = obj(raw);
	const list = Array.isArray(raw) ? raw : Array.isArray(r.clis) ? r.clis : [];
	return list
		.map((c): CLIProvider => {
			const o = obj(c);
			const name = str(o.name);
			const auth = str(o.auth);
			const raw = str(o.status);
			const installed = o.installed !== false && raw !== "missing";
			const status: CLIStatus = !installed
				? "missing"
				: raw === "warn"
					? "warn"
					: "ok";
			return {
				name,
				installed,
				status,
				binary: str(o.binary),
				path: str(o.path),
				version: str(o.version),
				versionNote: str(o.versionNote),
				signedIn: installed && (o.signed_in === true || auth === "signed in"),
				auth,
				quota: str(o.quota),
				tracksQuota: o.tracksQuota === true,
				quotaError: str(o.quotaError),
				models: strings(o.models).map((m) =>
					m.includes("/") ? m : `${name}/${m}`,
				),
				modelNote: str(o.modelNote),
				defaultModel: str(o.defaultModel ?? o.default_model),
				projectsNote: str(o.projectsNote),
			};
		})
		.filter((c) => c.name);
}

/** The one-line sign-in state a CLI row shows. */
export function cliAuthLabel(cli: CLIProvider): string {
	if (!cli.installed) return "not installed";
	if (cli.signedIn) return "signed in";
	return cli.auth || "unknown";
}
