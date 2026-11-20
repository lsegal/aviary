<template>
	<div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900"
		data-testid="cli-providers">
		<h3
			class="mb-2 flex items-center gap-2 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
			Coding CLIs
			<span v-if="refreshing" data-testid="cli-providers-refreshing"
				class="ml-auto inline-flex items-center gap-1.5 text-xs font-normal normal-case tracking-normal text-gray-500">
				<span class="h-1.5 w-1.5 animate-pulse rounded-full bg-blue-500" /> Checking
			</span>
		</h3>
		<p class="mb-4 text-xs text-gray-500 dark:text-gray-400">
			Coding agents Aviary runs through their own command-line tools. Each signs in with its own CLI; this list shows
			what Aviary finds on this machine.
		</p>

		<p v-if="!clis && refreshing" class="text-xs text-gray-500">Checking coding CLIs…</p>
		<p v-else-if="error && !clis" class="text-xs text-red-600 dark:text-red-400">
			Couldn't read the coding CLIs: {{ error }}
		</p>
		<div v-if="clis?.length" class="overflow-hidden rounded-lg border border-gray-200 dark:border-gray-700">
			<table class="w-full text-xs">
				<thead>
					<tr class="border-b border-gray-200 bg-gray-50 dark:border-gray-700 dark:bg-gray-800/50">
						<th class="px-3 py-2 text-left font-medium text-gray-500 dark:text-gray-400">CLI</th>
						<th class="px-3 py-2 text-left font-medium text-gray-500 dark:text-gray-400">Install</th>
						<th class="px-3 py-2 text-left font-medium text-gray-500 dark:text-gray-400">Sign-in</th>
						<th class="px-3 py-2 text-left font-medium text-gray-500 dark:text-gray-400">Quota</th>
						<th class="px-3 py-2 text-left font-medium text-gray-500 dark:text-gray-400">Models</th>
					</tr>
				</thead>
				<tbody>
					<tr v-for="cli in clis" :key="cli.name" :data-cli="cli.name" :data-status="cli.status"
						class="border-b border-gray-100 align-top last:border-0 dark:border-gray-800">
						<td class="px-3 py-2">
							<div class="flex items-center gap-2">
								<span class="h-2 w-2 shrink-0 rounded-full" :class="DOT[cli.status]" />
								<span class="font-medium text-gray-800 dark:text-gray-200">{{ cli.name }}</span>
								<span
									class="rounded bg-violet-100 px-1.5 py-0.5 text-[10px] font-medium text-violet-700 dark:bg-violet-900/30 dark:text-violet-300">CLI</span>
							</div>
						</td>
						<td class="px-3 py-2" data-field="install">
							<template v-if="cli.installed">
								<div class="text-gray-700 dark:text-gray-300">{{ cli.version || "version unknown" }}</div>
								<div class="break-all font-mono text-[11px] text-gray-400 dark:text-gray-500">{{ cli.path || cli.binary }}
								</div>
								<div v-if="cli.versionNote" class="text-[11px] text-amber-600 dark:text-amber-400">{{ cli.versionNote }}
								</div>
							</template>
							<template v-else>
								<div class="text-gray-500 dark:text-gray-400">Not installed</div>
								<div class="font-mono text-[11px] text-gray-400 dark:text-gray-500">{{ cli.binary }} not on PATH</div>
							</template>
						</td>
						<td class="px-3 py-2" data-field="auth">
							<span :class="cli.signedIn
								? 'text-green-600 dark:text-green-400'
								: cli.installed && cli.auth === 'signed out'
									? 'text-amber-600 dark:text-amber-400'
									: 'text-gray-500 dark:text-gray-400'">{{ cliAuthLabel(cli) }}</span>
						</td>
						<td class="px-3 py-2 text-gray-600 dark:text-gray-300" data-field="quota">
							<template v-if="cli.installed">
								{{ cli.quota || "—" }}
								<div v-if="cli.quotaError" class="text-[11px] text-amber-600 dark:text-amber-400">{{ cli.quotaError }}</div>
							</template>
							<template v-else>—</template>
						</td>
						<td class="px-3 py-2" data-field="models">
							<details v-if="cli.models.length">
								<summary class="cursor-pointer text-gray-700 dark:text-gray-300">
									{{ cli.models.length }} {{ cli.models.length === 1 ? "model" : "models" }}
									<span v-if="cli.defaultModel" class="text-gray-400 dark:text-gray-500">· default {{ cli.defaultModel }}</span>
								</summary>
								<ul class="mt-1 space-y-0.5 font-mono text-[11px] text-gray-500 dark:text-gray-400">
									<li v-for="m in cli.models" :key="m">{{ m }}</li>
								</ul>
								<div v-if="cli.modelNote" class="mt-1 text-[11px] text-gray-400 dark:text-gray-500">{{ cli.modelNote }}</div>
							</details>
							<span v-else class="text-gray-500 dark:text-gray-400">{{ cli.modelNote || "—" }}</span>
						</td>
					</tr>
				</tbody>
			</table>
		</div>
		<button type="button"
			class="mt-2 text-xs text-gray-400 hover:text-gray-600 disabled:opacity-50 dark:text-gray-500 dark:hover:text-gray-300"
			:disabled="refreshing" @click="refresh">↻ Check again</button>
	</div>
</template>

<script setup lang="ts">
import { useCLIProviders } from "../../composables/useCLIProviders";
import { type CLIStatus, cliAuthLabel } from "../../lib/cliProviders";

const DOT: Record<CLIStatus, string> = {
	ok: "bg-green-500",
	warn: "bg-amber-500",
	missing: "bg-gray-300 dark:bg-gray-600",
};

const { clis, refreshing, error, refresh } = useCLIProviders();
</script>
