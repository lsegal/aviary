// The Code Loop board's Queue choices (#1095): fix an issue with gh-fix,
// safely merge pull requests with gh-safe-merge, or answer a Discussion with
// gh-discuss. The header's Queue menu lists them, and so does its "…" menu
// once Queue has moved there on a phone (#1576).
import ArrowsRightLeftIcon from "@heroicons/vue/24/outline/ArrowsRightLeftIcon";
import ChatBubbleLeftEllipsisIcon from "@heroicons/vue/24/outline/ChatBubbleLeftEllipsisIcon";
import WrenchScrewdriverIcon from "@heroicons/vue/24/outline/WrenchScrewdriverIcon";

/** The Queue choices; no_merge leaves pull requests for a human. */
export function queueOptions(noMerge?: boolean) {
	return [
		{
			skill: "gh-fix" as const,
			label: "Fix issue",
			icon: WrenchScrewdriverIcon,
			disabled: false,
			title: "Have gh-fix fix issues",
		},
		{
			skill: "gh-safe-merge" as const,
			label: "Safe merge PR",
			icon: ArrowsRightLeftIcon,
			disabled: noMerge,
			title: noMerge
				? "Safe merge is off: the project's no_merge setting leaves pull requests for a human to merge"
				: "Queue pull requests for gh-safe-merge; they verify and merge side by side, starting in the order picked",
		},
		{
			skill: "gh-discuss" as const,
			label: "Discuss",
			icon: ChatBubbleLeftEllipsisIcon,
			disabled: false,
			title: "Queue Discussion threads for gh-discuss",
		},
	];
}
