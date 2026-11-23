<template>
	<section data-testid="transcription-status"
		class="mb-6 rounded-2xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
		<div class="flex flex-wrap items-start justify-between gap-3">
			<div>
				<h3 class="text-base font-semibold text-gray-900 dark:text-white">Voice transcription</h3>
				<p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
					Turns channel voice messages and composer recordings into text with a local
					whisper.cpp engine.
					<template v-if="status">
						<span v-if="!status.enabled">Turned off in aviary.yaml.</span>
						<span v-else>Language: {{ status.language }}.</span>
					</template>
				</p>
			</div>
			<button v-if="status && !status.ready" type="button" data-testid="transcription-install"
				class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-500 disabled:opacity-50"
				:disabled="installing" @click="install">
				{{ installing ? "Installing…" : "Install" }}
			</button>
		</div>
		<p v-if="error" role="alert" class="mt-3 text-sm text-red-600 dark:text-red-400">{{ error }}</p>
		<ul v-if="status" class="mt-4 grid gap-2 sm:grid-cols-3">
			<li v-for="row in rows" :key="row.label" :data-testid="`transcription-${row.key}`"
				class="min-w-0 rounded-xl border border-gray-100 px-3 py-2 dark:border-gray-800">
				<div class="flex items-center gap-2 text-sm font-medium text-gray-900 dark:text-white">
					<span class="h-2 w-2 rounded-full" :class="row.ok ? 'bg-green-500' : 'bg-red-500'" aria-hidden="true" />
					{{ row.label }}
					<span class="ml-auto text-xs font-normal" :class="row.ok ? 'text-green-700 dark:text-green-400' : 'text-red-600 dark:text-red-400'">
						{{ row.state }}
					</span>
				</div>
				<p v-if="row.path" class="mt-1 truncate font-mono text-xs text-gray-500 dark:text-gray-400" :title="row.path">
					{{ row.path }}
				</p>
			</li>
		</ul>
	</section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useAuthStore } from "../stores/auth";

interface Binary {
	name: string;
	installed: boolean;
	path?: string;
}

interface TranscriptionStatus {
	enabled: boolean;
	engine: string;
	language: string;
	ready: boolean;
	missing?: string[];
	install?: string;
	error?: string;
	status: {
		whisper: Binary;
		ffmpeg: Binary;
		model: { name: string; downloaded: boolean; path: string };
	};
}

const auth = useAuthStore();
const status = ref<TranscriptionStatus | null>(null);
const error = ref("");
const installing = ref(false);

const rows = computed(() => {
	const s = status.value?.status;
	if (!s) return [];
	return [
		{
			key: "engine",
			label: "whisper-cli",
			ok: s.whisper.installed,
			state: s.whisper.installed ? "Installed" : "Missing",
			path: s.whisper.path,
		},
		{
			key: "model",
			label: `Model ${s.model.name}`,
			ok: s.model.downloaded,
			state: s.model.downloaded ? "Downloaded" : "Missing",
			path: s.model.path,
		},
		{
			key: "ffmpeg",
			label: "ffmpeg",
			ok: s.ffmpeg.installed,
			state: s.ffmpeg.installed ? "Installed" : "Missing",
			path: s.ffmpeg.path,
		},
	];
});

async function request(method: "GET" | "POST") {
	const token = auth.getToken();
	const res = await fetch("/api/transcription", {
		method,
		headers: token ? { Authorization: `Bearer ${token}` } : {},
	});
	if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
	const data = (await res.json()) as TranscriptionStatus;
	status.value = data;
	error.value = data.error ?? "";
}

async function load() {
	try {
		await request("GET");
	} catch (e) {
		error.value = `Couldn't read transcription status: ${e instanceof Error ? e.message : String(e)}`;
	}
}

async function install() {
	installing.value = true;
	error.value = "";
	try {
		await request("POST");
	} catch (e) {
		error.value = `Install failed: ${e instanceof Error ? e.message : String(e)}`;
	} finally {
		installing.value = false;
	}
}

onMounted(() => {
	void load();
});
</script>
