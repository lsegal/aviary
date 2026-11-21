// Shared classes for AppSelect and AppCombobox, so every dropdown's trigger
// matches the app's text inputs and every popover matches its trigger.

export type SelectSize = "md" | "sm";

/** The field box: input height, radius, border, focus ring; chevron inset by the right padding. */
export const FIELD: Record<SelectSize, string> = {
	md: "rounded-lg px-3 py-2 text-sm min-h-[38px] [@media(pointer:coarse)]:min-h-11",
	sm: "rounded-md px-2 py-1 text-xs min-h-[26px] [@media(pointer:coarse)]:min-h-9",
};

export const FIELD_BASE =
	"group flex w-full items-center gap-2 border bg-white text-left text-gray-900 transition-colors hover:border-gray-400 focus:outline-none focus-visible:border-blue-500 focus-visible:ring-1 focus-visible:ring-blue-500 data-[disabled]:cursor-not-allowed data-[disabled]:opacity-60 dark:bg-gray-800 dark:text-white dark:hover:border-gray-600";

export const FIELD_BORDER =
	"border-gray-300 data-[state=open]:border-blue-500 data-[state=open]:ring-1 data-[state=open]:ring-blue-500 dark:border-gray-700";

export const FIELD_INVALID =
	"border-red-300 text-red-900 hover:border-red-400 focus-visible:border-red-500 focus-visible:ring-red-500 dark:border-red-800 dark:text-red-100";

export const CHEVRON =
	"select-chevron h-4 w-4 shrink-0 text-gray-400 transition-transform duration-150 group-data-[state=open]:rotate-180 motion-reduce:transition-none dark:text-gray-500";

/** The popover: same radius as its trigger, at least as wide, scrolls and fits the screen. */
export const CONTENT: Record<SelectSize, string> = {
	md: "rounded-lg",
	sm: "rounded-md",
};

export const CONTENT_BASE =
	"app-popover z-[60] overflow-hidden border border-gray-200 bg-white text-gray-900 shadow-lg dark:border-gray-700 dark:bg-gray-900 dark:text-gray-100";

export const ITEM: Record<SelectSize, string> = {
	md: "rounded-md px-2.5 py-2 text-sm [@media(pointer:coarse)]:min-h-11",
	sm: "rounded px-2 py-1 text-xs [@media(pointer:coarse)]:min-h-10",
};

export const ITEM_BASE =
	"relative flex cursor-pointer select-none items-center gap-2 text-gray-700 outline-none data-[disabled]:cursor-default data-[disabled]:opacity-40 data-[highlighted]:bg-blue-50 data-[highlighted]:text-blue-700 data-[state=checked]:font-medium dark:text-gray-200 dark:data-[highlighted]:bg-blue-900/40 dark:data-[highlighted]:text-blue-200";

export const DESCRIPTION =
	"block text-xs font-normal text-gray-500 group-data-[highlighted]/item:text-blue-600 dark:text-gray-400 dark:group-data-[highlighted]/item:text-blue-300";

export const GROUP_LABEL =
	"px-2.5 pb-1 pt-2 text-[11px] font-semibold uppercase tracking-wide text-gray-400 dark:text-gray-500";

export const EMPTY =
	"px-2.5 py-2 text-sm italic text-gray-400 dark:text-gray-500";

export const CHECK = "h-4 w-4 shrink-0 text-blue-600 dark:text-blue-400";
