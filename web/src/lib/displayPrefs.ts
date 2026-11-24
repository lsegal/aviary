// The web UI's display preferences (#1093): its color theme and text size.
// index.html applies the cached copy before the first paint with the same
// attributes, so a page load never flashes the wrong theme or size.

export type Theme = "light" | "dark" | "system";
export type TextSize = "small" | "default" | "large" | "xlarge";

/** Every display preference; an option added later gets a default here. */
export interface DisplayPrefs {
	theme: Theme;
	textSize: TextSize;
}

export const THEMES: { value: Theme; label: string }[] = [
	{ value: "light", label: "Light" },
	{ value: "dark", label: "Dark" },
	{ value: "system", label: "System" },
];

export const TEXT_SIZES: { value: TextSize; label: string }[] = [
	{ value: "small", label: "Small" },
	{ value: "default", label: "Default" },
	{ value: "large", label: "Large" },
	{ value: "xlarge", label: "Extra large" },
];

export const DISPLAY_STORAGE_KEY = "aviary.display.prefs";

export function defaultDisplayPrefs(): DisplayPrefs {
	return { theme: "system", textSize: "default" };
}

/** prefs as sent by the server or stored, with unknown values defaulted. */
export function normalizeDisplayPrefs(raw: unknown): DisplayPrefs {
	const prefs = defaultDisplayPrefs();
	if (!raw || typeof raw !== "object") return prefs;
	const { theme, textSize } = raw as Record<string, unknown>;
	if (THEMES.some((t) => t.value === theme)) prefs.theme = theme as Theme;
	if (TEXT_SIZES.some((s) => s.value === textSize))
		prefs.textSize = textSize as TextSize;
	return prefs;
}

export function isDefaultDisplay(prefs: DisplayPrefs): boolean {
	const defaults = defaultDisplayPrefs();
	return (Object.keys(defaults) as (keyof DisplayPrefs)[]).every(
		(k) => prefs[k] === defaults[k],
	);
}

/** The theme shown: theme itself, or the OS's when it follows the system. */
export function resolvedTheme(
	theme: Theme,
	systemDark: boolean,
): "light" | "dark" {
	if (theme === "system") return systemDark ? "dark" : "light";
	return theme;
}

/** Shows prefs on the page: style.css keys the theme and size off these. */
export function applyDisplayPrefs(prefs: DisplayPrefs, systemDark: boolean) {
	const root = document.documentElement;
	root.dataset.theme = resolvedTheme(prefs.theme, systemDark);
	root.dataset.textSize = prefs.textSize;
}
