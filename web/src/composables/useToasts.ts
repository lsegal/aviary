import { ref } from "vue";

/** A short notice in the corner of the screen; error toasts report a failure. */
export interface Toast {
	id: number;
	message: string;
	tone: "error" | "info";
}

// One stack for the whole app, shown by ToastStack in AppLayout.
const toasts = ref<Toast[]>([]);
let nextID = 1;

const TOAST_MS = 6_000;

export function useToasts() {
	function dismiss(id: number) {
		toasts.value = toasts.value.filter((t) => t.id !== id);
	}
	function show(message: string, tone: Toast["tone"] = "info") {
		const id = nextID++;
		toasts.value = [...toasts.value, { id, message, tone }];
		setTimeout(() => dismiss(id), TOAST_MS);
	}
	return { toasts, show, dismiss };
}
