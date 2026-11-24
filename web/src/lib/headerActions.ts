// The page header's action buttons (#1147), the same on every page: labeled
// actions are outlined icon-and-label buttons, then icon-only buttons.

/** An outlined icon-and-label action, such as "Run now". */
export const HEADER_ACTION_CLS =
	"inline-flex shrink-0 items-center gap-1 rounded-lg border border-gray-200 px-2.5 py-1 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800";

/** An outlined icon-only action, such as the chat toggle or the gear. */
export const HEADER_ICON_CLS =
	"inline-flex shrink-0 items-center justify-center rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800";

/** An icon-only toggle that is on, such as an open chat panel. */
export const HEADER_ICON_ON_CLS =
	"inline-flex shrink-0 items-center justify-center rounded-lg border border-blue-600 bg-blue-50 p-1.5 text-blue-700 dark:border-blue-500 dark:bg-blue-950/50 dark:text-blue-300";
