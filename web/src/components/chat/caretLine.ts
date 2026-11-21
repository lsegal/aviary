// Finds which visual line of a textarea holds the caret, counting soft-wrapped
// lines as well as newlines. A hidden mirror div lays the text out with the
// textarea's own font, padding, and wrapping width, and marker spans report
// the top of the first line, the caret's line, and the last line.

const copiedStyles = [
	"paddingTop",
	"paddingRight",
	"paddingBottom",
	"paddingLeft",
	"fontFamily",
	"fontSize",
	"fontStyle",
	"fontVariant",
	"fontWeight",
	"fontStretch",
	"letterSpacing",
	"lineHeight",
	"textIndent",
	"textTransform",
	"wordSpacing",
	"tabSize",
] as const;

// A zero-width space gives a marker a line box without taking up width, so
// it never wraps onto a line of its own.
const zeroWidth = "​";

export interface CaretLine {
	first: boolean;
	last: boolean;
}

/** Reports whether the caret is on the textarea's first and last visual lines. */
export function caretLine(el: HTMLTextAreaElement): CaretLine {
	const style = getComputedStyle(el);
	const mirror = document.createElement("div");
	for (const prop of copiedStyles) mirror.style[prop] = style[prop];
	mirror.style.position = "absolute";
	mirror.style.visibility = "hidden";
	mirror.style.top = "0";
	mirror.style.left = "-9999px";
	mirror.style.whiteSpace = "pre-wrap";
	mirror.style.overflowWrap = "break-word";
	mirror.style.wordBreak = style.wordBreak;
	mirror.style.border = "0";
	mirror.style.boxSizing = "content-box";
	// clientWidth leaves out the scrollbar, which narrows the wrapping width.
	const paddingX =
		Number.parseFloat(style.paddingLeft) + Number.parseFloat(style.paddingRight);
	mirror.style.width = `${el.clientWidth - paddingX}px`;

	const caret = el.selectionStart;
	const start = marker(zeroWidth);
	// The caret span holds the text after the caret, so its top is the line
	// the next character starts on, which is where the browser draws a caret
	// sitting at a wrap point.
	const at = marker(el.value.slice(caret) || zeroWidth);
	const end = marker(zeroWidth);
	mirror.append(start, el.value.slice(0, caret), at, end);
	document.body.append(mirror);
	try {
		const half = start.offsetHeight / 2;
		return {
			first: at.offsetTop - start.offsetTop < half,
			last: end.offsetTop - at.offsetTop < half,
		};
	} finally {
		mirror.remove();
	}
}

function marker(text: string): HTMLSpanElement {
	const span = document.createElement("span");
	span.textContent = text;
	return span;
}
