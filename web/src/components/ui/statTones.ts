/** The colors a StatCard can take. */
export type StatTone = "neutral" | "green" | "purple" | "blue" | "red";

export const STAT_TONES: Record<StatTone, string> = {
	neutral: "border-gray-200 text-gray-900 dark:border-gray-800 dark:text-white",
	green:
		"border-green-200 bg-green-50 text-green-700 dark:border-green-900 dark:bg-green-950/40 dark:text-green-300",
	purple:
		"border-purple-200 bg-purple-50 text-purple-700 dark:border-purple-900 dark:bg-purple-950/40 dark:text-purple-300",
	blue: "border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900 dark:bg-blue-950/40 dark:text-blue-300",
	red: "border-red-200 bg-red-50 text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300",
};
