import { afterEach, describe, expect, it, vi } from "vitest";
import {
	dedupeToolMessages,
	extractCompleteToolPayload,
	formatDateLabel,
	formatTime,
	isAudioMedia,
	isErrorMessage,
	isImageMedia,
	isVideoMedia,
	type Message,
	mediaTypeFromURL,
	parseToolChunk,
	parseToolMessage,
	savedAudioURL,
	toolName,
} from "./chatMessages";

// #1607: the chat view's message helpers.

describe("isErrorMessage", () => {
	it("flags error and missing-provider replies only", () => {
		expect(isErrorMessage("Error: boom")).toBe(true);
		expect(isErrorMessage("[no LLM provider configured]")).toBe(true);
		expect(isErrorMessage("All good. Error: none")).toBe(false);
	});
});

describe("parseToolMessage", () => {
	it("parses a JSON tool record", () => {
		const msg = parseToolMessage(
			'[tool] {"id":"t1","name":"web_search","args":{"q":"x"},"result":"ok"}',
			"2026-01-01T00:00:00Z",
			"gpt",
			"m1",
		);
		expect(msg).toEqual({
			id: "m1",
			role: "tool",
			text: "web_search",
			toolData: {
				id: "t1",
				name: "web_search",
				args: { q: "x" },
				result: "ok",
			},
			timestamp: "2026-01-01T00:00:00Z",
			model: "gpt",
		});
	});

	it("keeps a bare name or a nameless record as raw text", () => {
		expect(parseToolMessage("[tool] browser_open")).toEqual({
			id: undefined,
			role: "tool",
			text: "browser_open",
			timestamp: undefined,
			model: undefined,
		});
		expect(parseToolMessage('[tool] {"args":{}}').toolData).toBeUndefined();
		expect(parseToolMessage('[tool] {"args":{}}').text).toBe('{"args":{}}');
	});
});

describe("parseToolChunk", () => {
	it("parses a streamed tool event", () => {
		expect(
			parseToolChunk('{"id":"a","name":"x","done":true}', "ts", "i"),
		).toEqual({
			id: "i",
			role: "tool",
			text: "x",
			toolData: { id: "a", name: "x", done: true },
			timestamp: "ts",
		});
	});

	it("falls back to the raw chunk", () => {
		expect(parseToolChunk("not json")).toEqual({
			id: undefined,
			role: "tool",
			text: "not json",
			timestamp: undefined,
		});
		expect(parseToolChunk("{}").toolData).toBeUndefined();
	});
});

describe("dedupeToolMessages", () => {
	it("keeps only the latest record per tool-use ID", () => {
		const list: Message[] = [
			{ role: "user", text: "hi" },
			{ role: "tool", text: "a", toolData: { id: "1", name: "a" } },
			{ role: "tool", text: "b", toolData: { name: "b" } },
			{ role: "tool", text: "b", toolData: { name: "b" } },
			{
				role: "tool",
				text: "a",
				toolData: { id: "1", name: "a", result: "done" },
			},
			{ role: "assistant", text: "ok" },
		];
		const out = dedupeToolMessages(list);
		expect(out).toHaveLength(5);
		expect(out[0].text).toBe("hi");
		expect(out[1].toolData?.id).toBeUndefined();
		expect(out[3].toolData?.result).toBe("done");
		expect(out[4].text).toBe("ok");
	});
});

describe("extractCompleteToolPayload", () => {
	it("needs a [tool] prefix and a payload", () => {
		expect(extractCompleteToolPayload("hello")).toBeNull();
		expect(extractCompleteToolPayload("[tool] ")).toBeNull();
	});

	it("splits a bare name at the newline", () => {
		expect(extractCompleteToolPayload("[tool] browser")).toBeNull();
		expect(extractCompleteToolPayload("[tool] browser\nrest")).toEqual({
			payload: "browser",
			rest: "rest",
		});
	});

	it("returns a complete JSON object and what follows it", () => {
		const json = '{"name":"x","args":{"q":"a } \\" {"}}';
		expect(extractCompleteToolPayload(`[tool] ${json}tail`)).toEqual({
			payload: json,
			rest: "tail",
		});
	});

	it("waits for an unfinished object", () => {
		expect(
			extractCompleteToolPayload('[tool] {"name":"x","args":{'),
		).toBeNull();
		expect(extractCompleteToolPayload('[tool] {"name":"x\\')).toBeNull();
	});
});

describe("formatDateLabel", () => {
	afterEach(() => {
		vi.useRealTimers();
	});

	it("names today and yesterday", () => {
		vi.useFakeTimers();
		vi.setSystemTime(new Date(2026, 5, 15, 12));
		expect(formatDateLabel(new Date(2026, 5, 15, 1))).toBe("Today");
		expect(formatDateLabel(new Date(2026, 5, 14, 23))).toBe("Yesterday");
	});

	it("adds the year only for other years", () => {
		vi.useFakeTimers();
		vi.setSystemTime(new Date(2026, 5, 15, 12));
		const sameYear = formatDateLabel(new Date(2026, 0, 2));
		expect(sameYear).not.toMatch(/2026/);
		expect(sameYear).toBe(
			new Date(2026, 0, 2).toLocaleDateString(undefined, {
				weekday: "short",
				month: "short",
				day: "numeric",
			}),
		);
		expect(formatDateLabel(new Date(2024, 0, 2))).toMatch(/2024/);
	});
});

describe("formatTime", () => {
	it("formats hours and minutes in the local time zone", () => {
		const ts = "2026-03-04T05:06:07Z";
		expect(formatTime(ts)).toBe(
			new Date(ts).toLocaleTimeString(undefined, {
				hour: "numeric",
				minute: "2-digit",
			}),
		);
	});
});

describe("savedAudioURL", () => {
	it("leaves non-file URLs alone", () => {
		expect(savedAudioURL("https://x/a.ogg", "tok")).toBe("https://x/a.ogg");
	});

	it("maps saved audio to the media endpoint with the token", () => {
		expect(savedAudioURL("file:///tmp/voice%20note.ogg", "tok")).toBe(
			"/api/media?path=%2Ftmp%2Fvoice+note.ogg&token=tok",
		);
		expect(savedAudioURL("file:///C:/a/b.mp3")).toBe(
			"/api/media?path=C%3A%2Fa%2Fb.mp3",
		);
	});

	it("keeps other files and malformed URLs as they are", () => {
		expect(savedAudioURL("file:///tmp/doc.pdf", "tok")).toBe(
			"file:///tmp/doc.pdf",
		);
		expect(savedAudioURL("file:///%E0%A4%A.ogg")).toBe("file:///%E0%A4%A.ogg");
	});
});

describe("media classification", () => {
	it("reads the media type from the URL", () => {
		expect(mediaTypeFromURL("/api/media?path=x")).toBe("audio/*");
		expect(mediaTypeFromURL("data:image/png;base64,AAA")).toBe("image/png");
		expect(mediaTypeFromURL("data:;base64,AAA")).toBe("");
		expect(mediaTypeFromURL(" HTTPS://x/A.PNG?x=1 ")).toBe("image/*");
		expect(mediaTypeFromURL("https://x/a.mp4#t")).toBe("video/*");
		expect(mediaTypeFromURL("https://x/a.mp3")).toBe("audio/*");
		expect(mediaTypeFromURL("https://x/a.pdf")).toBe("");
	});

	it("classifies images, video and audio", () => {
		expect(isImageMedia("a.webp")).toBe(true);
		expect(isImageMedia("a.mp4")).toBe(false);
		expect(isVideoMedia("a.mov")).toBe(true);
		expect(isVideoMedia("a.wav")).toBe(false);
		expect(isAudioMedia("a.flac")).toBe(true);
		expect(isAudioMedia("a.png")).toBe(false);
	});
});

describe("toolName", () => {
	it("prefers the parsed name over the raw text", () => {
		expect(
			toolName({ role: "tool", text: "raw", toolData: { name: "x" } }),
		).toBe("x");
		expect(toolName({ role: "tool", text: "raw" })).toBe("raw");
	});
});
