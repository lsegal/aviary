import { afterEach, beforeEach, expect, test } from "vitest";
import { page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { note, setup, stubAudio } from "./helpers/notificationServer";

// The pages this file renders (see component-test/routes.ts).
declarePages(() => import("../views/OverviewView.vue"));

// Its own file (#1636): useNotifications loads the saved settings (sound
// on) once per page, and the sound player keeps its first AudioContext, so
// both must start fresh.

const realAudioContext = window.AudioContext;

beforeEach(async () => {
	await page.viewport(1280, 900);
});

afterEach(() => {
	window.AudioContext = realAudioContext;
});

test("sound plays the notification's sound at the chosen volume, and is silent by default", async () => {
	const server = setup({
		prefs: { sound: { enabled: true, type: "chime", volume: 30 } },
	});
	const played = stubAudio();
	await renderApp("/system/overview");
	// The server leaves sound empty while sound is off.
	await server.push(note({ title: "quiet one", sound: "" }));
	await expect
		.poll(() => page.getByTestId("notification-toast").elements().length)
		.toBe(1);
	expect(played).toHaveLength(0);

	await server.push(note({ title: "loud one", sound: "ping" }));
	await expect.poll(() => played).toEqual([{ gain: 0.12, freqs: [1568] }]);
});
