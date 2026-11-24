<script setup lang="ts">
// frame is a frame of the real aviary watch TUI, rendered from its model by
// TestWatchTUILandingFrame in cmd/aviary/cmd, so the mock follows the TUI.
import frame from "./watch-tui-frame.json";

interface FrameSpan {
	t: string;
	fg?: string;
	b?: boolean;
	u?: boolean;
	r?: boolean;
}

const termFg = "#e6e3e0";
const termBg = "#111113";
const rows = frame.rows as FrameSpan[][];

function spanStyle(span: FrameSpan) {
	const style: Record<string, string> = {};
	if (span.r) {
		style.color = termBg;
		style.background = span.fg ?? termFg;
	} else if (span.fg) {
		style.color = span.fg;
	}
	if (span.b) style.fontWeight = "700";
	if (span.u) style.textDecoration = "underline";
	return style;
}
</script>

<template>
	<figure class="watch-tui" :style="{ '--cols': frame.cols }">
		<div class="watch-tui-bar">
			<span class="tl" aria-hidden="true"><span /><span /><span /></span>
			<span class="watch-tui-title">aviary watch acme/storefront</span>
		</div>
		<div class="watch-tui-screen" tabindex="0"
			aria-label="aviary watch terminal board with the prompt chat open">
			<pre><template v-for="(row, i) in rows" :key="i"><span v-for="(span, j) in row" :key="j"
				:style="spanStyle(span)">{{ span.t }}</span>{{ "\n" }}</template></pre>
		</div>
	</figure>
</template>

<style scoped>
.watch-tui {
	margin: 0;
	min-width: 0;
	overflow: hidden;
	border: 1px solid #26262a;
	border-radius: 14px;
	background: #111113;
	box-shadow: 0 20px 40px -20px rgba(24, 20, 19, 0.45);
}

.watch-tui-bar {
	display: flex;
	align-items: center;
	gap: 12px;
	padding: 9px 14px;
	border-bottom: 1px solid #26262a;
	background: #19191c;
	color: #94908d;
	font-family: var(--mono);
	font-size: 12px;
}

.watch-tui-bar .tl {
	display: flex;
	gap: 6px;
}

.watch-tui-bar .tl span {
	width: 10px;
	height: 10px;
	border-radius: 999px;
}

.watch-tui-bar .tl span:nth-child(1) {
	background: #ec6a5f;
}

.watch-tui-bar .tl span:nth-child(2) {
	background: #f5bd4f;
}

.watch-tui-bar .tl span:nth-child(3) {
	background: #62c554;
}

.watch-tui-title {
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

/* The screen fits its frame's width down to a legible font size, then
   scrolls inside the frame. */
.watch-tui-screen {
	container-type: inline-size;
	overflow-x: auto;
	padding: 12px 14px;
}

.watch-tui-screen:focus-visible {
	outline: 2px solid var(--brand);
	outline-offset: -2px;
}

.watch-tui-screen pre {
	width: max-content;
	margin: 0;
	color: #e6e3e0;
	font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
	font-size: max(7px, calc(100cqw / (var(--cols) * 0.602)));
	line-height: 1.3;
	white-space: pre;
}
</style>
