import { useAgentsStore } from "../stores/agents";

// Skipping the setup wizard lasts for the browser session, so a skip is the
// user's choice for this visit rather than a permanent opt-out of onboarding.
const SKIP_KEY = "aviary_onboarding_skipped";

// Once an agent has been seen, setup is complete and the gate stops asking.
let configured = false;

export function skipOnboarding() {
	sessionStorage.setItem(SKIP_KEY, "1");
}

/**
 * Whether the user still has to go through the setup wizard: no agent exists
 * and the wizard has not been skipped. A detected provider (an installed CLI,
 * a stored key) does not count; only an agent completes setup.
 */
export async function needsOnboarding(): Promise<boolean> {
	if (configured || sessionStorage.getItem(SKIP_KEY)) return false;
	const agents = useAgentsStore();
	await agents.fetchAgents();
	// Never trap the user in the wizard when the agent list cannot be read.
	if (agents.error) return false;
	configured = agents.agents.length > 0;
	return !configured;
}
