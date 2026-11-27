// The page header's action buttons (#1147), the same on every page: labeled
// actions are outlined icon-and-label buttons, then icon-only buttons. All of
// them, and any select in the header, are 36px tall so the row lines up
// (#1347).

/** A header select's trigger, as tall as the buttons beside it (#1347). */
export const HEADER_SELECT_CLS = "h-9 min-h-9! py-1";

/** An outlined icon-and-label action, such as "Run now". */
export const HEADER_ACTION_CLS =
	"inline-flex h-9 shrink-0 items-center justify-center gap-1 rounded-lg border border-gray-200 px-2.5 py-1 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800";

/** An outlined icon-only action, such as the chat toggle or the gear. */
export const HEADER_ICON_CLS =
	"inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800";

/** The blue "+" add action, such as the Code Loop board's Add project (#739, #1197). */
export const HEADER_ADD_CLS =
	"inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-blue-600 bg-blue-600 p-1.5 text-white hover:bg-blue-700 disabled:opacity-50 dark:border-blue-500 dark:bg-blue-600 dark:hover:bg-blue-500";

/** An icon-only toggle that is on, such as an open chat panel. */
export const HEADER_ICON_ON_CLS =
	"inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-blue-600 bg-blue-50 p-1.5 text-blue-700 dark:border-blue-500 dark:bg-blue-950/50 dark:text-blue-300";

// A labeled action that collapses to an icon-only button below the `sm`
// breakpoint (#1374), so a phone header fits like its icon-only neighbors.
// Put the icon in HEADER_COLLAPSE_ICON_CLS, the label in a
// HEADER_COLLAPSE_LABEL_CLS span, and give the button an aria-label and title.

/** A labeled action that is icon-only on phones, such as "Schedule". */
export const HEADER_COLLAPSE_CLS =
	"inline-flex h-9 w-9 shrink-0 items-center justify-center gap-1 rounded-lg border border-gray-200 p-1.5 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50 sm:w-auto sm:px-2.5 sm:py-1 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800";

/** The icon of a HEADER_COLLAPSE_CLS action: icon-button sized on phones. */
export const HEADER_COLLAPSE_ICON_CLS = "h-5 w-5 sm:h-4 sm:w-4";

/** The label of a HEADER_COLLAPSE_CLS action: hidden on phones. */
export const HEADER_COLLAPSE_LABEL_CLS = "hidden sm:inline";

/**
 * The warning dot on a header icon whose contents need attention, such as the
 * Code Loop details icon when a CLI provider is signed out (#1580). The icon
 * must be positioned (`relative`).
 */
export const ALERT_DOT_CLS =
	"pointer-events-none absolute top-0 right-0 h-2 w-2 rounded-full bg-amber-500 ring-2 ring-white dark:ring-gray-900";
