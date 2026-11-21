import { ref } from "vue";

export interface ChannelDialog {
	mode: "create" | "remove";
	agent: string;
	/** The channel being removed. */
	channel?: string;
}

// One dialog at a time, opened from the Channels row's "+" and a channel's
// "…" Remove, and shown by ChannelDialogs.
const dialog = ref<ChannelDialog | null>(null);

export function useChannelDialogs() {
	return {
		dialog,
		open(next: ChannelDialog) {
			dialog.value = next;
		},
		close() {
			dialog.value = null;
		},
	};
}
