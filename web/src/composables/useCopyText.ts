// Copies text to the clipboard and keeps a short status for a live region,
// such as the Code Loop board's instance ID (#1570, #1576).
import { type Ref, ref } from "vue";

export function useCopyText(failed: string): {
	status: Ref<string>;
	copy: (text: string) => Promise<void>;
} {
	const status = ref("");
	async function copy(text: string) {
		try {
			await navigator.clipboard.writeText(text);
			status.value = "Copied";
		} catch {
			status.value = failed;
		}
	}
	return { status, copy };
}
