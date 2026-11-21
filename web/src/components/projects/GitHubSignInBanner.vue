<template>
	<section v-if="session.signedOut || session.signingIn" data-testid="github-signin-banner" role="alert"
		class="rounded-xl border border-amber-300 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-200">
		<div class="flex flex-col gap-3 lg:flex-row lg:items-start">
			<div class="min-w-0 flex-1">
				<p class="font-semibold">
					{{ session.signingIn ? "Waiting for you to sign in to GitHub" : "Signed out of GitHub in Aviary's browser" }}
				</p>
				<p v-if="session.signingIn" class="mt-1">
					A sign-in window is open in Aviary's browser. Sign in there and Code Loop picks up where it left off.
				</p>
				<p v-else class="mt-1">
					<template v-if="session.httpStatus">GitHub answered {{ session.httpStatus }}</template>
					<template v-else>GitHub served a signed-out page</template>
					<template v-if="session.url"> for <a :href="session.url" target="_blank" rel="noopener noreferrer"
							class="break-all font-mono text-xs underline">{{ session.url }}</a></template>,
					so private repositories and <code class="font-mono">@me</code> searches show nothing. This is not "no issues".
				</p>
				<p v-if="!session.signingIn && session.windowUnavailable" data-testid="github-signin-no-window" class="mt-1">
					No window can open here ({{ session.windowUnavailable }}). Switch to gh polling, or import an Aviary
					browser profile signed in to GitHub on another machine.
				</p>
				<p v-if="session.lastError" class="mt-1 text-red-700 dark:text-red-300">{{ session.lastError }}</p>
			</div>
			<div v-if="!session.signingIn" class="flex flex-wrap items-center gap-2">
				<button v-if="!session.windowUnavailable" type="button" :disabled="busy"
					class="rounded-lg bg-amber-600 px-3 py-1.5 font-medium text-white hover:bg-amber-700 disabled:opacity-50"
					@click="emit('signin')">
					Sign in to GitHub
				</button>
				<button type="button" :disabled="busy"
					class="rounded-lg border border-amber-400 px-3 py-1.5 font-medium hover:bg-amber-100 disabled:opacity-50 dark:border-amber-700 dark:hover:bg-amber-900/40"
					@click="emit('usePolling')">
					Switch to gh polling
				</button>
			</div>
		</div>
		<form v-if="!session.signingIn && session.windowUnavailable" class="mt-3 flex flex-col gap-2 sm:flex-row"
			@submit.prevent="profile.trim() && emit('import', profile.trim())">
			<label class="sr-only" for="github-profile-import">Aviary browser profile to import</label>
			<input id="github-profile-import" v-model="profile" type="text" placeholder="/path/to/aviary/browser"
				class="min-w-0 flex-1 rounded-lg border border-amber-300 bg-white px-3 py-1.5 font-mono text-xs text-gray-900 dark:border-amber-800 dark:bg-gray-900 dark:text-gray-100" />
			<button type="submit" :disabled="busy || !profile.trim()"
				class="rounded-lg border border-amber-400 px-3 py-1.5 font-medium hover:bg-amber-100 disabled:opacity-50 dark:border-amber-700 dark:hover:bg-amber-900/40">
				Import profile
			</button>
		</form>
	</section>
</template>

<script setup lang="ts">
import { ref } from "vue";
import type { GitHubSession } from "../../lib/projects";

defineProps<{ session: GitHubSession; busy: boolean }>();
const emit = defineEmits<{
	signin: [];
	usePolling: [];
	import: [profile: string];
}>();
const profile = ref("");
</script>
