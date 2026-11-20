import type { ApprovalMode } from "../stores/settings";

/** The approval modes, least to most permissive, as settings pages offer them. */
export const APPROVAL_MODES: { value: ApprovalMode; label: string }[] = [
	{ value: "read-only", label: "Read-only: inspect, change nothing" },
	{ value: "auto", label: "Auto: work in the workspace, CLI sandbox on" },
	{ value: "full", label: "Full: no sandbox or permission checks" },
];
