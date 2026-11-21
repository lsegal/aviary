/** A file attached to a chat message, as the data URL sent to agent_run. */
export interface ChatAttachment {
	id: string;
	name: string;
	/** Images are sent to the model as images; other files as text. */
	kind: "image" | "text";
	dataURL: string;
}

/** Largest file the composer attaches. */
export const MAX_ATTACHMENT_BYTES = 10 * 1024 * 1024;

let nextID = 0;

function readAs(
	file: Blob,
	as: "dataURL" | "buffer",
): Promise<string | ArrayBuffer> {
	return new Promise((resolve, reject) => {
		const reader = new FileReader();
		reader.onload = () => resolve(reader.result ?? "");
		reader.onerror = () => reject(reader.error);
		if (as === "dataURL") reader.readAsDataURL(file);
		else reader.readAsArrayBuffer(file);
	});
}

function decodeText(buffer: ArrayBuffer): string | null {
	const bytes = new Uint8Array(buffer);
	if (bytes.includes(0)) return null;
	try {
		return new TextDecoder("utf-8", { fatal: true }).decode(bytes);
	} catch {
		return null;
	}
}

function base64(text: string): string {
	let binary = "";
	for (const b of new TextEncoder().encode(text)) {
		binary += String.fromCharCode(b);
	}
	return btoa(binary);
}

/** Reads a picked, pasted or dropped file into an attachment. Images and
 * UTF-8 text files are supported; anything else returns an error message. */
export async function readAttachment(
	file: File,
): Promise<{ attachment: ChatAttachment } | { error: string }> {
	const name = file.name || (file.type.startsWith("image/") ? "image" : "file");
	if (file.size > MAX_ATTACHMENT_BYTES) {
		return { error: `${name} is larger than 10 MB.` };
	}
	nextID += 1;
	const id = `att-${nextID}`;
	if (file.type.startsWith("image/")) {
		const dataURL = (await readAs(file, "dataURL")) as string;
		return { attachment: { id, name, kind: "image", dataURL } };
	}
	const text = decodeText((await readAs(file, "buffer")) as ArrayBuffer);
	if (text === null) {
		return {
			error: `${name} can't be attached: only images and text files are supported.`,
		};
	}
	const dataURL = `data:text/plain;name=${encodeURIComponent(name)};base64,${base64(text)}`;
	return { attachment: { id, name, kind: "text", dataURL } };
}

/** The file name a text attachment's data URL carries, if any. */
export function attachmentName(mediaURL: string): string {
	const comma = mediaURL.indexOf(",");
	const head = comma === -1 ? mediaURL : mediaURL.slice(0, comma);
	for (const param of head.split(";").slice(1)) {
		if (param.startsWith("name=")) {
			try {
				return decodeURIComponent(param.slice("name=".length));
			} catch {
				return param.slice("name=".length);
			}
		}
	}
	return "";
}
