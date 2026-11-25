import { coverageEnabled, coverageReport } from "./coverage";

// Merges the coverage every worker added into one report after the run.
export default async function globalTeardown() {
	if (!coverageEnabled) return;
	await coverageReport().generate();
}
