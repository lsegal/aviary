import { ref } from "vue";

/** A short notice in the corner of the screen; error toasts report a failure. */
export interface Toast {
	id: number;
	message: string;
	tone: "error" | "info";
	/** A notification's toast (#1215) shows its title over its body, and
	 * opens its page when clicked. */
	title?: string;
	body?: string;
	onClick?: () => void;
	testId?: string;
}

export type ToastOptions = Pick<Toast, "title" | "body" | "onClick" | "testId">;

// One stack for the whole app, shown by ToastStack in AppLayout.
const toasts = ref<Toast[]>([]);
let nextID = 1;

const TOAST_MS = 6_000;

export function useToasts() {
	function dismiss(id: number) {
		toasts.value = toasts.value.filter((t) => t.id !== id);
	}
	function show(
		message: string,
		tone: Toast["tone"] = "info",
		options: ToastOptions = {},
	) {
		const id = nextID++;
		toasts.value = [...toasts.value, { id, message, tone, ...options }];
		setTimeout(() => dismiss(id), TOAST_MS);
	}
	return { toasts, show, dismiss };
}
