<template>
	<DialogRoot :open="!!skill" @update:open="(v) => { if (!v && !queueing) emit('close') }">
		<DialogPortal>
			<DialogOverlay class="fixed inset-0 z-50 bg-black/50" />
			<DialogContent data-testid="queue-dialog" :data-skill="skill"
				class="fixed left-1/2 top-1/2 z-50 flex max-h-[85vh] w-[calc(100%-2rem)] max-w-3xl -translate-x-1/2 -translate-y-1/2 flex-col rounded-xl bg-white p-6 shadow-2xl dark:bg-gray-900">
				<DialogTitle class="text-base font-bold text-gray-900 dark:text-white">{{ copy.title }}</DialogTitle>
				<DialogDescription class="mt-1 text-sm text-gray-600 dark:text-gray-400">{{ copy.description }}</DialogDescription>

				<p v-if="mergeOff" data-testid="queue-no-merge" role="alert"
					class="mt-3 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-300">
					Safe merge is off: the project's <code class="font-mono">no_merge</code> setting leaves pull requests for a human to merge.
				</p>

				<label v-if="skill === 'gh-fix'" class="mt-4 block text-sm">
					<span class="text-gray-700 dark:text-gray-200">Issue numbers</span>
					<input v-model="issueText" data-testid="queue-issues" type="text" placeholder="#1201, #1207"
						class="field-input mt-1" @keydown.enter.prevent="submit" />
				</label>
				<template v-else>
					<div class="relative mt-4">
						<span class="pointer-events-none absolute inset-y-0 left-3 flex items-center text-gray-400" aria-hidden="true">
							<MagnifyingGlassIcon class="h-4 w-4" />
						</span>
						<input v-model="search" type="search" data-testid="queue-search" :aria-label="`Search ${copy.noun}s`"
							:placeholder="`Search ${copy.noun}s by number, title or author`" class="field-input pl-9!" />
					</div>
					<div class="mt-3 min-h-0 flex-1 overflow-auto rounded-lg border border-gray-200 dark:border-gray-800">
						<p v-if="loading" class="p-4 text-sm text-gray-500">Loading {{ copy.noun }}s…</p>
						<p v-else-if="!shown.length" class="p-4 text-sm text-gray-500">No open {{ copy.noun }}s{{ search ? " match" : "" }}.</p>
						<table v-else class="w-full text-left text-sm" data-testid="queue-candidates">
							<thead class="sticky top-0 bg-gray-50 text-xs uppercase tracking-wide text-gray-500 dark:bg-gray-800 dark:text-gray-400">
								<tr>
									<th class="w-10 px-3 py-2"><span class="sr-only">Order</span></th>
									<th class="px-3 py-2">{{ copy.noun === "pull request" ? "Pull request" : "Discussion" }}</th>
									<th class="px-3 py-2">Author</th>
									<template v-if="skill === 'gh-safe-merge'">
										<th class="px-3 py-2">CI</th>
										<th class="px-3 py-2">Review</th>
										<th class="px-3 py-2">Mergeable</th>
									</template>
									<template v-else>
										<th class="px-3 py-2">Category</th>
										<th class="px-3 py-2">Replies</th>
									</template>
									<th class="px-3 py-2">Job</th>
								</tr>
							</thead>
							<tbody>
								<tr v-for="c in shown" :key="c.number" data-testid="queue-candidate" :data-number="c.number"
									:aria-disabled="!!c.ownedBy || mergeOff"
									class="border-t border-gray-100 dark:border-gray-800"
									:class="c.ownedBy || mergeOff ? 'opacity-60' : 'cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-800/60'"
									@click="toggle(c)">
									<td class="px-3 py-2">
										<span v-if="order(c)" data-testid="queue-order"
											class="inline-flex h-6 w-6 items-center justify-center rounded-full bg-blue-600 text-xs font-bold text-white">{{ order(c) }}</span>
										<input v-else type="checkbox" :disabled="!!c.ownedBy || mergeOff" :aria-label="`Queue #${c.number}`"
											class="h-4 w-4" @click.stop="toggle(c)" />
									</td>
									<td class="max-w-xs px-3 py-2">
										<a :href="c.url" target="_blank" rel="noreferrer" class="text-gray-500 hover:underline" @click.stop>#{{ c.number }}</a>
										<span class="ml-1 text-gray-900 dark:text-gray-100">{{ c.title }}</span>
										<span v-if="c.draft" class="ml-1 rounded bg-gray-100 px-1 text-xs text-gray-500 dark:bg-gray-800">draft</span>
									</td>
									<td class="px-3 py-2 text-gray-600 dark:text-gray-300">{{ c.author }}</td>
									<template v-if="skill === 'gh-safe-merge'">
										<td class="px-3 py-2"><span :class="TONE[c.ci]">{{ LABELS[c.ci] ?? c.ci }}</span></td>
										<td class="px-3 py-2"><span :class="TONE[c.review]">{{ LABELS[c.review] ?? c.review }}</span></td>
										<td class="px-3 py-2"><span :class="TONE[c.mergeable]">{{ LABELS[c.mergeable] ?? c.mergeable }}</span></td>
									</template>
									<template v-else>
										<td class="px-3 py-2 text-gray-600 dark:text-gray-300">{{ c.category }}</td>
										<td class="px-3 py-2 text-gray-600 dark:text-gray-300">{{ c.answered ? "answered" : c.comments }}</td>
									</template>
									<td class="px-3 py-2 text-xs">
										<span v-if="c.ownedBy" data-testid="queue-owned" class="text-amber-700 dark:text-amber-400">{{ c.ownedBy }}</span>
										<span v-else class="text-gray-400">—</span>
									</td>
								</tr>
							</tbody>
						</table>
					</div>
					<p v-if="skill === 'gh-safe-merge'" class="mt-2 text-xs text-gray-500 dark:text-gray-400">
						Merges run one at a time in the order picked, each against the latest default branch.
					</p>
				</template>

				<ul v-if="results.length" data-testid="queue-results" class="mt-3 space-y-1 text-sm">
					<li v-for="r in results" :key="r.number" :class="r.queued ? 'text-green-700 dark:text-green-400' : 'text-amber-700 dark:text-amber-400'">
						#{{ r.number }}: {{ resultText(r) }}
					</li>
				</ul>
				<p v-if="error" role="alert" class="mt-3 text-sm text-red-600 dark:text-red-400">{{ error }}</p>

				<div class="mt-5 flex justify-end gap-3">
					<DialogClose :disabled="queueing"
						class="rounded-lg border border-gray-200 px-4 py-2 text-sm text-gray-600 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-400 dark:hover:bg-gray-800">
						{{ results.length ? "Close" : "Cancel" }}
					</DialogClose>
					<button type="button" data-testid="queue-submit" :disabled="queueing || !chosen.length || mergeOff"
						class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-50"
						@click="submit">{{ submitLabel }}</button>
				</div>
			</DialogContent>
		</DialogPortal>
	</DialogRoot>
</template>

<script setup lang="ts">
// The Queue menu's picker (#1095): open pull requests for gh-safe-merge, with
// CI, review, mergeability, author and the job that owns each, or open
// Discussion threads for gh-discuss, searched and picked in order; or issue
// numbers for gh-fix. A row a job already owns cannot be picked.
import MagnifyingGlassIcon from "@heroicons/vue/24/outline/MagnifyingGlassIcon";
import {
	DialogClose,
	DialogContent,
	DialogDescription,
	DialogOverlay,
	DialogPortal,
	DialogRoot,
	DialogTitle,
} from "radix-vue";
import { computed, ref, watch } from "vue";
import { useMCP } from "../../composables/useMCP";
import {
	normalizeQueueCandidates,
	normalizeQueueResults,
	parseIssueNumbers,
	type QueueCandidate,
	type QueueResult,
	type QueueSkill,
} from "../../lib/projects";

const props = defineProps<{
	target: string;
	/** The skill to queue for; the dialog is open while it is set. */
	skill: QueueSkill | null;
	noMerge?: boolean;
}>();
const emit = defineEmits<{ close: []; queued: [] }>();

const { callTool } = useMCP();
const candidates = ref<QueueCandidate[]>([]);
const picked = ref<number[]>([]);
const issueText = ref("");
const search = ref("");
const loading = ref(false);
const queueing = ref(false);
const error = ref("");
const results = ref<QueueResult[]>([]);
const listedNoMerge = ref(false);

const COPY: Record<
	QueueSkill,
	{ title: string; description: string; noun: string }
> = {
	"gh-fix": {
		title: "Fix issues",
		description:
			"gh-fix fixes each issue end to end. An issue a job already owns is not queued twice.",
		noun: "issue",
	},
	"gh-safe-merge": {
		title: "Safe merge pull requests",
		description:
			"gh-safe-merge safety-checks each pull request, drives its CI green, and merges it.",
		noun: "pull request",
	},
	"gh-discuss": {
		title: "Discuss",
		description:
			"gh-discuss reads each thread and the repository, then posts one answer or stays silent.",
		noun: "discussion",
	},
};
const copy = computed(() => COPY[props.skill ?? "gh-fix"]);

const LABELS: Record<string, string> = {
	passing: "passing",
	failing: "failing",
	pending: "pending",
	none: "—",
	approved: "approved",
	changes_requested: "changes requested",
	review_required: "review required",
	mergeable: "clean",
	conflicting: "conflicts",
	unknown: "unknown",
};
const TONE: Record<string, string> = {
	passing: "text-green-700 dark:text-green-400",
	approved: "text-green-700 dark:text-green-400",
	mergeable: "text-green-700 dark:text-green-400",
	failing: "text-red-600 dark:text-red-400",
	changes_requested: "text-red-600 dark:text-red-400",
	conflicting: "text-red-600 dark:text-red-400",
	pending: "text-amber-700 dark:text-amber-400",
	review_required: "text-amber-700 dark:text-amber-400",
	none: "text-gray-400",
	unknown: "text-gray-400",
};

const mergeOff = computed(
	() =>
		props.skill === "gh-safe-merge" && (props.noMerge || listedNoMerge.value),
);

const shown = computed(() => {
	const q = search.value.trim().toLowerCase().replace(/^#/, "");
	if (!q) return candidates.value;
	return candidates.value.filter(
		(c) =>
			String(c.number).startsWith(q) ||
			c.title.toLowerCase().includes(q) ||
			c.author.toLowerCase().includes(q),
	);
});

const chosen = computed(() =>
	props.skill === "gh-fix" ? parseIssueNumbers(issueText.value) : picked.value,
);

const submitLabel = computed(() => {
	if (queueing.value) return "Queueing…";
	const n = chosen.value.length;
	const noun = props.skill === "gh-safe-merge" ? "PR" : copy.value.noun;
	return n ? `Queue ${n} ${n === 1 ? noun : `${noun}s`}` : "Queue";
});

function order(c: QueueCandidate) {
	const index = picked.value.indexOf(c.number);
	return index < 0 ? 0 : index + 1;
}

function toggle(c: QueueCandidate) {
	if (c.ownedBy || mergeOff.value) return;
	const index = picked.value.indexOf(c.number);
	if (index < 0) picked.value = [...picked.value, c.number];
	else picked.value = picked.value.filter((n) => n !== c.number);
}

function resultText(r: QueueResult) {
	if (r.queued)
		return r.position
			? `queued, #${r.position} in the merge lane`
			: r.reason || "queued";
	if (r.ownedBy) return `already owned by job ${r.ownedBy}; not queued again`;
	return r.reason;
}

watch(
	() => props.skill,
	async (skill) => {
		error.value = "";
		results.value = [];
		search.value = "";
		candidates.value = [];
		listedNoMerge.value = false;
		picked.value = [];
		issueText.value = "";
		if (!skill || skill === "gh-fix") return;
		loading.value = true;
		try {
			const listed = normalizeQueueCandidates(
				JSON.parse(
					await callTool("project_queue_candidates", {
						target: props.target,
						skill,
					}),
				),
			);
			candidates.value = listed.candidates;
			listedNoMerge.value = listed.noMerge;
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		} finally {
			loading.value = false;
		}
	},
	{ immediate: true },
);

async function submit() {
	if (!props.skill || !chosen.value.length || mergeOff.value) return;
	queueing.value = true;
	error.value = "";
	try {
		const titles = Object.fromEntries(
			candidates.value
				.filter((c) => chosen.value.includes(c.number))
				.map((c) => [String(c.number), c.title]),
		);
		results.value = normalizeQueueResults(
			JSON.parse(
				await callTool("project_job_queue", {
					target: props.target,
					skill: props.skill,
					numbers: chosen.value,
					titles,
				}),
			),
		);
		picked.value = [];
		issueText.value = "";
		emit("queued");
	} catch (e) {
		error.value = e instanceof Error ? e.message : String(e);
	} finally {
		queueing.value = false;
	}
}
</script>
