/**
 * Board-load timing (#1661). Each Code Loop board load records performance
 * marks and measures, so a regression shows up in the browser's performance
 * timeline and in tests as numbers: how long its requests took, how long
 * parsing and applying the responses took, and how long until the board
 * rendered them.
 */
export const BOARD_LOAD_MEASURES = {
	/** From the load's start until every response arrived. */
	request: "aviary:board-load:request",
	/** Parsing the responses and applying them to the board's state. */
	parse: "aviary:board-load:parse",
	/** From the state change until Vue rendered it. */
	render: "aviary:board-load:render",
	/** The whole load. */
	total: "aviary:board-load",
} as const;

const MARKS = {
	start: "aviary:board-load:start",
	response: "aviary:board-load:response",
	parsed: "aviary:board-load:parsed",
	rendered: "aviary:board-load:rendered",
} as const;

type Phase = keyof typeof MARKS;

function available() {
	return (
		typeof performance !== "undefined" &&
		typeof performance.mark === "function" &&
		typeof performance.measure === "function"
	);
}

/**
 * Marks a phase of the current board load. The start clears the previous
 * load's entries, so the timeline holds only the latest load's.
 */
export function markBoardLoad(phase: Phase) {
	if (!available()) return;
	if (phase === "start") {
		for (const name of Object.values(MARKS)) performance.clearMarks(name);
		for (const name of Object.values(BOARD_LOAD_MEASURES))
			performance.clearMeasures(name);
	}
	performance.mark(MARKS[phase]);
	try {
		switch (phase) {
			case "response":
				performance.measure(
					BOARD_LOAD_MEASURES.request,
					MARKS.start,
					MARKS.response,
				);
				break;
			case "parsed":
				performance.measure(
					BOARD_LOAD_MEASURES.parse,
					MARKS.response,
					MARKS.parsed,
				);
				break;
			case "rendered":
				performance.measure(
					BOARD_LOAD_MEASURES.render,
					MARKS.parsed,
					MARKS.rendered,
				);
				performance.measure(
					BOARD_LOAD_MEASURES.total,
					MARKS.start,
					MARKS.rendered,
				);
				break;
		}
	} catch {
		// A phase whose earlier mark was cleared by a newer load measures
		// nothing; the newer load records its own.
	}
}
