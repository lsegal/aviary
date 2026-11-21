import type { Component } from "vue";
import MarkdownPreview from "./MarkdownPreview.vue";

// Previewers for the Files page, keyed by file extension. Each component
// takes the file's current text as its `content` prop. Supporting a new
// format is one registerFilePreviewer call; FilePreviewSplit does not change.

export interface FilePreviewer {
	id: string;
	label: string;
	/** Lowercase extensions without the dot, such as "md". */
	extensions: string[];
	component: Component;
}

const byExtension = new Map<string, FilePreviewer>();

export function registerFilePreviewer(previewer: FilePreviewer) {
	for (const ext of previewer.extensions)
		byExtension.set(ext.toLowerCase(), previewer);
}

/** The previewer for a file name, or undefined when it has none. */
export function findFilePreviewer(fileName: string | null | undefined) {
	const dot = fileName?.lastIndexOf(".") ?? -1;
	if (!fileName || dot < 0) return undefined;
	return byExtension.get(fileName.slice(dot + 1).toLowerCase());
}

registerFilePreviewer({
	id: "markdown",
	label: "Markdown",
	extensions: ["md", "markdown"],
	component: MarkdownPreview,
});
