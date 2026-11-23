<template>
  <div class="space-y-4" data-testid="more-analytics-panel">
    <div data-testid="jobs-by-day" class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
      <div class="mb-3 flex items-center justify-between">
        <h3 class="text-sm font-semibold text-gray-700 dark:text-gray-300">Jobs by Day</h3>
        <div class="flex items-center gap-3 text-[10px] text-gray-500">
          <span class="flex items-center gap-1"><span class="inline-block h-2 w-2 rounded-sm bg-green-500" /> Completed</span>
          <span class="flex items-center gap-1"><span class="inline-block h-2 w-2 rounded-sm bg-red-400" /> Failed</span>
          <span class="flex items-center gap-1"><span class="inline-block h-2 w-2 rounded-sm bg-blue-400" /> Other</span>
        </div>
      </div>
      <div v-if="!store.byDay.some(d => d.completed + d.failed + d.running > 0)"
        class="flex h-20 items-center justify-center text-xs text-gray-400">
        No jobs in selected range
      </div>
      <div v-else>
        <div class="flex items-end gap-px overflow-hidden" style="height:64px">
          <div v-for="(row, i) in store.byDay" :key="row.date"
            class="group relative flex flex-1 cursor-default flex-col justify-end"
            style="height:64px"
            @mouseenter="hoveredDay = i"
            @mouseleave="hoveredDay = null">
            <div v-if="hoveredDay === i"
              class="pointer-events-none absolute bottom-full left-1/2 z-10 mb-1 -translate-x-1/2 whitespace-nowrap rounded bg-gray-900 px-2 py-1 text-[10px] text-white shadow dark:bg-gray-700">
              <div class="font-semibold">{{ row.date }}</div>
              <div v-if="row.completed">✓ {{ row.completed }}</div>
              <div v-if="row.failed">✗ {{ row.failed }}</div>
              <div v-if="row.running">● {{ row.running }}</div>
            </div>
            <div v-if="dayH(row, 'running') > 0" class="w-full bg-blue-400 dark:bg-blue-600"
              :style="{ height: dayH(row, 'running') + 'px' }" />
            <div v-if="dayH(row, 'failed') > 0" class="w-full bg-red-400 dark:bg-red-600"
              :style="{ height: dayH(row, 'failed') + 'px' }" />
            <div v-if="dayH(row, 'completed') > 0" class="w-full bg-green-500 dark:bg-green-600"
              :style="{ height: dayH(row, 'completed') + 'px' }" />
          </div>
        </div>
        <div class="mt-1 flex overflow-hidden">
          <span v-for="(row, i) in store.byDay" :key="row.date"
            class="flex-1 truncate text-center text-[9px] text-gray-400">
            {{ showLabel(i, store.byDay.length) ? row.date.slice(5) : "" }}
          </span>
        </div>
      </div>
    </div>

    <!-- Jobs table -->
    <div ref="queueSection" data-testid="job-queue" class="rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900">
      <div class="flex flex-wrap items-center gap-3 border-b border-gray-100 px-5 py-3 dark:border-gray-800">
        <h3 class="text-sm font-semibold text-gray-700 dark:text-gray-300">Job Queue</h3>
        <div role="group" aria-label="Job status" class="flex overflow-hidden rounded-lg border border-gray-200 text-xs dark:border-gray-700">
          <button v-for="s in statusFilters" :key="s.value" type="button" :aria-pressed="statusFilter === s.value"
            class="px-2.5 py-1 transition-colors"
            :class="statusFilter === s.value
              ? 'bg-blue-600 text-white'
              : 'bg-white text-gray-600 hover:bg-gray-100 dark:bg-gray-900 dark:text-gray-400 dark:hover:bg-gray-800'"
            @click="statusFilter = s.value">
            {{ s.label }}
          </button>
        </div>
        <span v-if="task" data-testid="job-task-filter"
          class="inline-flex items-center gap-1 rounded-full bg-blue-50 px-2.5 py-0.5 text-xs font-medium text-blue-700 dark:bg-blue-950/40 dark:text-blue-300">
          {{ task }}
          <button type="button" aria-label="Show every task's runs" class="hover:text-blue-900 dark:hover:text-blue-100" @click="emit('update:task', '')">✕</button>
        </span>
        <span class="text-xs text-gray-500 dark:text-gray-400" data-testid="job-count">{{ filteredJobs.length }} jobs</span>
        <div class="ml-auto flex items-center gap-3 text-xs text-gray-400">
          <span>{{ jobsRangeLabel }}</span>
          <label class="flex items-center gap-2">
            <span>Per page</span>
            <AppSelect v-model="jobsPageSize" size="sm" class="w-auto" :options="PAGE_SIZE_OPTIONS" />
          </label>
        </div>
      </div>
      <div v-if="!filteredJobs.length" class="px-5 py-8 text-center text-sm text-gray-400">
        No jobs in this range.
      </div>
      <div v-else class="overflow-x-auto">
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-gray-100 text-gray-400 dark:border-gray-800">
              <th class="px-5 py-2.5 font-medium">Status</th>
              <th class="px-4 py-2.5 font-medium">Task</th>
              <th class="px-4 py-2.5 font-medium">Created</th>
              <th class="px-4 py-2.5 font-medium">Duration</th>
              <th class="px-4 py-2.5 text-right font-medium">Attempts</th>
              <th class="px-4 py-2.5 font-medium">Scheduled</th>
              <th class="px-4 py-2.5 text-right font-medium">Action</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="job in paginatedJobs" :key="job.id"
              class="cursor-pointer border-b border-gray-50 hover:bg-gray-50 dark:border-gray-800 dark:hover:bg-gray-800/50"
              :class="selectedJob?.id === job.id ? 'bg-blue-50 dark:bg-blue-950/20' : ''"
              @click="selectJob(job)">
              <td class="px-5 py-2.5">
                <span class="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[10px] font-semibold"
                  :class="statusClass(job.status)">
                  <span v-if="job.status === 'in_progress'" class="h-1 w-1 animate-pulse rounded-full bg-current" />
                  {{ statusLabel(job.status) }}
                </span>
              </td>
              <td class="px-4 py-2.5">
                <code class="font-mono text-gray-600 dark:text-gray-400">{{ job.task_id }}</code>
              </td>
              <td class="px-4 py-2.5 text-gray-400">{{ fmtTs(job.created_at) }}</td>
              <td class="px-4 py-2.5 font-mono text-gray-500">{{ duration(job) }}</td>
              <td class="px-4 py-2.5 text-right text-gray-500">{{ job.attempts }}/{{ job.max_retries }}</td>
              <td class="px-4 py-2.5 text-gray-400">
                {{ job.scheduled_for ? fmtTs(job.scheduled_for) : "—" }}
              </td>
              <td class="px-4 py-2.5 text-right">
                <button
                  v-if="job.status === 'pending'"
                  class="rounded-lg bg-blue-600 px-3 py-1.5 text-[11px] font-medium text-white hover:bg-blue-700 disabled:opacity-50"
                  :disabled="runningJobID === job.id"
                  @click.stop="runQueuedJobNow(job.id)">
                  {{ runningJobID === job.id ? "Running…" : "Run Now" }}
                </button>
                <button
                  v-else-if="job.status === 'in_progress'"
                  class="rounded-lg bg-red-600 px-3 py-1.5 text-[11px] font-medium text-white hover:bg-red-700 disabled:opacity-50"
                  :disabled="stoppingJobID === job.id"
                  @click.stop="stopJob(job.id)">
                  {{ stoppingJobID === job.id ? "Stopping…" : "Stop" }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div
        v-if="filteredJobs.length"
        class="flex flex-wrap items-center justify-between gap-3 border-t border-gray-100 px-5 py-3 text-xs dark:border-gray-800"
      >
        <span class="text-gray-400">{{ jobsRangeLabel }}</span>
        <div class="flex items-center gap-2">
          <button
            class="rounded-lg border border-gray-200 px-3 py-1.5 text-gray-600 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
            :disabled="jobsPage <= 1"
            @click="jobsPage = 1"
          >
            First
          </button>
          <button
            class="rounded-lg border border-gray-200 px-3 py-1.5 text-gray-600 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
            :disabled="jobsPage <= 1"
            @click="jobsPage--"
          >
            Prev
          </button>
          <span class="min-w-24 text-center text-gray-500 dark:text-gray-400">
            Page {{ jobsPage }} of {{ totalJobPages }}
          </span>
          <button
            class="rounded-lg border border-gray-200 px-3 py-1.5 text-gray-600 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
            :disabled="jobsPage >= totalJobPages"
            @click="jobsPage++"
          >
            Next
          </button>
          <button
            class="rounded-lg border border-gray-200 px-3 py-1.5 text-gray-600 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
            :disabled="jobsPage >= totalJobPages"
            @click="jobsPage = totalJobPages"
          >
            Last
          </button>
        </div>
      </div>
    </div>

    <div data-testid="compile-attempts" class="rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900">
      <div class="flex items-center justify-between border-b border-gray-100 px-5 py-3 dark:border-gray-800">
        <h3 class="text-sm font-semibold text-gray-700 dark:text-gray-300">Compile Attempts</h3>
        <span class="text-xs text-gray-400">{{ visibleTaskCompiles.length }} shown of {{ store.taskCompiles.length }} recorded</span>
      </div>
      <div v-if="store.compilesLoading" class="px-5 py-6 text-sm text-gray-400">
        Loading compile attempts…
      </div>
      <div v-else-if="!store.taskCompiles.length" class="px-5 py-6 text-sm text-gray-400">
        No compile attempts in this range.
      </div>
      <div v-else class="overflow-x-auto">
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-gray-100 text-gray-400 dark:border-gray-800">
              <th class="px-5 py-2.5 font-medium">Status</th>
              <th class="px-4 py-2.5 font-medium">Task</th>
              <th class="px-4 py-2.5 font-medium">Created</th>
              <th class="px-4 py-2.5 font-medium">Result</th>
              <th class="px-4 py-2.5 font-medium">Reason</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="compile in visibleTaskCompiles" :key="compile.id"
              class="cursor-pointer border-b border-gray-50 hover:bg-gray-50 dark:border-gray-800 dark:hover:bg-gray-800/50"
              :class="selectedCompile?.id === compile.id ? 'bg-sky-50 dark:bg-sky-950/20' : ''"
              @click="selectCompile(compile)">
              <td class="px-5 py-2.5">
                <span class="inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-semibold"
                  :class="compileStatusClass(compile.status)">
                  {{ compileStatusLabel(compile.status) }}
                </span>
              </td>
              <td class="px-4 py-2.5">
                <div class="font-medium text-gray-700 dark:text-gray-200">
                  {{ compile.task_name || "ad hoc task" }}
                </div>
                <div class="font-mono text-[11px] text-gray-400">{{ compile.id }}</div>
              </td>
              <td class="px-4 py-2.5 text-gray-400">{{ fmtTs(compile.created_at) }}</td>
              <td class="px-4 py-2.5 font-mono text-gray-500">
                {{ compile.requested_task_type || "prompt" }} → {{ compile.result_task_type || "prompt" }}
              </td>
              <td class="max-w-xl truncate px-4 py-2.5 text-gray-600 dark:text-gray-400"
                :title="compile.reason || compile.prompt || ''">
                {{ compile.reason || compile.prompt || "—" }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Right: job detail panel -->
    <transition
      enter-active-class="transition-all duration-200 ease-out"
      leave-active-class="transition-all duration-150 ease-in"
      enter-from-class="opacity-0 translate-x-4"
      leave-to-class="opacity-0 translate-x-4">
      <div v-if="selectedJob || selectedCompile" data-testid="details-panel" :style="detailsPanel.style.value"
    class="fixed inset-y-0 right-0 z-40 flex w-96 max-w-full flex-col border-l border-gray-200 bg-white shadow-2xl dark:border-gray-800 dark:bg-gray-900">
        <div v-if="detailsPanel.desktop.value" data-testid="details-panel-resize" role="separator" tabindex="0"
          aria-orientation="vertical" aria-label="Resize details panel" :aria-valuenow="detailsPanel.width.value"
          :aria-valuemin="DETAILS_PANEL_MIN_WIDTH" :aria-valuemax="detailsPanel.maxWidth.value"
          title="Drag to resize, double-click to reset"
          class="absolute inset-y-0 -left-1 z-20 w-2 cursor-col-resize transition-colors hover:bg-blue-500/30 focus-visible:bg-blue-500/40 focus-visible:outline-none"
          :class="detailsPanel.resizing.value ? 'bg-blue-500/40' : ''"
          @pointerdown="detailsPanel.startResize" @dblclick="detailsPanel.resetWidth()"
          @keydown="detailsPanel.resizeByKey" />
        <!-- Panel header -->
        <div class="flex items-center justify-between border-b border-gray-100 px-5 py-3 dark:border-gray-800">
          <div v-if="selectedJob" class="flex items-center gap-2">
            <span class="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[10px] font-semibold"
              :class="statusClass(selectedJob.status)">
              <span v-if="selectedJob.status === 'in_progress'" class="h-1 w-1 animate-pulse rounded-full bg-current" />
              {{ statusLabel(selectedJob.status) }}
            </span>
            <code class="font-mono text-xs text-gray-500">…{{ selectedJob.id.slice(-12) }}</code>
          </div>
          <div v-else-if="selectedCompile" class="flex items-center gap-2">
            <span class="inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-semibold"
              :class="compileStatusClass(selectedCompile.status)">
              {{ compileStatusLabel(selectedCompile.status) }}
            </span>
            <code class="font-mono text-xs text-gray-500">…{{ selectedCompile.id.slice(-12) }}</code>
          </div>
          <button class="text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"
            @click="closeSelection">✕</button>
        </div>

        <!-- Metadata -->
        <div v-if="selectedJob" class="border-b border-gray-100 px-5 py-3 dark:border-gray-800">
          <dl class="grid grid-cols-2 gap-x-4 gap-y-2 text-xs">
            <div>
              <dt class="text-gray-400">Agent</dt>
              <dd class="font-medium text-gray-700 dark:text-gray-300">{{ selectedJob.agent_id }}</dd>
            </div>
            <div>
              <dt class="text-gray-400">Task</dt>
              <dd class="truncate font-mono text-gray-600 dark:text-gray-400">{{ selectedJob.task_id }}</dd>
            </div>
            <div>
              <dt class="text-gray-400">Created</dt>
              <dd class="text-gray-600 dark:text-gray-400">{{ fmtTs(selectedJob.created_at) }}</dd>
            </div>
            <div>
              <dt class="text-gray-400">Duration</dt>
              <dd class="font-mono text-gray-600 dark:text-gray-400">{{ duration(selectedJob) }}</dd>
            </div>
            <div v-if="selectedJob.scheduled_for" class="col-span-2">
              <dt class="text-gray-400">Scheduled for</dt>
              <dd class="text-gray-600 dark:text-gray-400">{{ fmtTs(selectedJob.scheduled_for) }}</dd>
            </div>
            <div>
              <dt class="text-gray-400">Attempts</dt>
              <dd class="text-gray-600 dark:text-gray-400">{{ selectedJob.attempts }} / {{ selectedJob.max_retries }}</dd>
            </div>
            <div v-if="selectedJob.next_retry_at">
              <dt class="text-gray-400">Next retry</dt>
              <dd class="text-gray-600 dark:text-gray-400">{{ fmtTs(selectedJob.next_retry_at) }}</dd>
            </div>
          </dl>
          <div v-if="selectedJob.error" role="alert" data-testid="job-error"
            class="mt-3 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
            <p class="mb-0.5 text-[10px] font-semibold uppercase tracking-wide">
              {{ selectedJob.status === 'failed' ? 'Error' : 'Last attempt failed' }}
            </p>
            <p class="whitespace-pre-wrap">{{ selectedJob.error }}</p>
          </div>
        </div>
        <div v-else-if="selectedCompile" class="border-b border-gray-100 px-5 py-3 dark:border-gray-800">
          <dl class="grid grid-cols-2 gap-x-4 gap-y-2 text-xs">
            <div>
              <dt class="text-gray-400">Agent</dt>
              <dd class="font-medium text-gray-700 dark:text-gray-300">{{ selectedCompile.agent_id }}</dd>
            </div>
            <div>
              <dt class="text-gray-400">Task</dt>
              <dd class="text-gray-600 dark:text-gray-400">{{ selectedCompile.task_name || "ad hoc task" }}</dd>
            </div>
            <div>
              <dt class="text-gray-400">Created</dt>
              <dd class="text-gray-600 dark:text-gray-400">{{ fmtTs(selectedCompile.created_at) }}</dd>
            </div>
            <div>
              <dt class="text-gray-400">Trigger</dt>
              <dd class="text-gray-600 capitalize dark:text-gray-400">{{ selectedCompile.trigger || "—" }}</dd>
            </div>
            <div>
              <dt class="text-gray-400">Result</dt>
              <dd class="font-mono text-gray-600 dark:text-gray-400">
                {{ selectedCompile.requested_task_type || "prompt" }} → {{ selectedCompile.result_task_type || "prompt" }}
              </dd>
            </div>
            <div>
              <dt class="text-gray-400">Validated</dt>
              <dd class="text-gray-600 dark:text-gray-400">{{ selectedCompile.validated ? "yes" : "no" }}</dd>
            </div>
            <div>
              <dt class="text-gray-400">Deterministic steps</dt>
              <dd class="text-gray-600 dark:text-gray-400">{{ selectedCompile.deterministic_steps ?? 0 }}</dd>
            </div>
            <div>
              <dt class="text-gray-400">Needs discovery</dt>
              <dd class="text-gray-600 dark:text-gray-400">{{ selectedCompile.needs_discovery ? "yes" : "no" }}</dd>
            </div>
            <div v-if="selectedCompile.target" class="col-span-2">
              <dt class="text-gray-400">Target</dt>
              <dd class="break-all text-gray-600 dark:text-gray-400">{{ selectedCompile.target }}</dd>
            </div>
          </dl>
        </div>

        <!-- Prompt -->
        <div v-if="selectedJob" class="border-b border-gray-100 px-5 py-3 dark:border-gray-800">
          <p class="mb-1 text-[10px] font-semibold uppercase tracking-wide text-gray-400">
            {{ selectedJobTaskType === "script" ? "Script" : "Prompt" }}
          </p>
          <pre v-if="selectedJobTaskType === 'script'"
            class="max-h-[40vh] overflow-auto rounded-lg bg-gray-950 p-3 font-mono text-[11px] text-green-400 whitespace-pre-wrap">{{ selectedJob.prompt }}</pre>
          <div v-else class="max-h-[40vh] overflow-y-auto rounded-lg bg-gray-50 p-3 dark:bg-gray-950/40">
            <p class="text-xs text-gray-600 dark:text-gray-300 whitespace-pre-wrap">{{ selectedJob.prompt }}</p>
          </div>
        </div>
        <div v-else-if="selectedCompile" class="border-b border-gray-100 px-5 py-3 dark:border-gray-800">
          <p class="mb-1 text-[10px] font-semibold uppercase tracking-wide text-gray-400">Prompt</p>
          <div class="max-h-[40vh] overflow-y-auto rounded-lg bg-gray-50 p-3 dark:bg-gray-950/40">
            <p class="text-xs text-gray-600 dark:text-gray-300 whitespace-pre-wrap">{{ selectedCompile.prompt || "—" }}</p>
          </div>
          <p v-if="selectedCompile.reason" class="mt-3 mb-1 text-[10px] font-semibold uppercase tracking-wide text-gray-400">Reason</p>
          <p v-if="selectedCompile.reason" class="text-xs text-gray-600 dark:text-gray-300">{{ selectedCompile.reason }}</p>
        </div>

        <!-- Output / logs -->
        <div v-if="selectedJob" class="flex min-h-0 flex-1 flex-col overflow-hidden px-5 py-3">
          <div class="mb-2 flex items-center justify-between">
            <p class="text-[10px] font-semibold uppercase tracking-wide text-gray-400">
              {{ selectedJob.status === 'in_progress' ? 'Live Output' : 'Output' }}
            </p>
            <button v-if="selectedJob.status !== 'in_progress'"
              class="text-[10px] text-blue-500 hover:text-blue-700"
              @click="reloadLogs">↺ Reload</button>
          </div>

          <!-- Running: live SSE log feed filtered by job_id -->
          <div v-if="selectedJob.status === 'in_progress'"
            class="flex-1 overflow-y-auto rounded-lg bg-gray-950 p-3 font-mono text-[11px] text-green-400">
            <div v-if="!liveLines.length" class="text-gray-500">
              <p>Waiting for output…</p>
              <p v-if="selectedJobIdleTimeout" class="mt-1">
                A run with no output for {{ selectedJobIdleTimeout }} is stopped and marked failed.
              </p>
            </div>
            <div v-for="(line, i) in liveLines" :key="i" class="whitespace-pre-wrap">{{ line }}</div>
            <div ref="liveBottom" />
          </div>

          <!-- Completed/failed: persisted output -->
          <div v-else
            class="flex-1 overflow-y-auto rounded-lg bg-gray-950 p-3 font-mono text-[11px]"
            :class="logsLoading ? 'text-gray-500' : 'text-green-400'">
            <div v-if="logsLoading">Loading…</div>
            <div v-else-if="!jobOutput" class="text-gray-500">
              <div v-if="sessionMessagesLoading">Loading session messages…</div>
              <div v-else-if="sessionMessages && sessionMessages.length">
                <div class="space-y-2">
                  <div v-for="(m, idx) in sessionMessages" :key="m.id ?? idx"
                    class="rounded-lg border border-gray-800/40 bg-gray-950 p-2 font-mono text-[11px]">
                    <div class="mb-1 flex items-center justify-between text-xs text-gray-400">
                      <div class="flex items-center gap-2">
                        <span class="inline-block rounded px-2 py-0.5 text-[10px] font-semibold"
                          :class="m.role === 'assistant' ? 'bg-green-900 text-green-300' : m.role === 'user' ? 'bg-gray-800 text-gray-300' : m.role === 'tool' ? 'bg-purple-900 text-purple-300' : 'bg-gray-700 text-gray-200'">
                          {{ m.role }}</span>
                        <span class="text-[11px] text-gray-400">{{ m.timestamp ? fmtTs(m.timestamp) : '' }}</span>
                      </div>
                      <button class="text-[11px] text-blue-400 hover:text-blue-200" @click="toggleExpanded(m.id ?? ('sess-'+idx))">
                        {{ isExpanded(m.id ?? ('sess-'+idx)) ? 'Hide' : 'Show' }}
                      </button>
                    </div>
                    <div class="whitespace-pre-wrap text-sm text-gray-200">
                      <div v-if="!isExpanded(m.id ?? ('sess-'+idx))">
                        <div v-if="m.media_urls?.length">
                          <a v-for="(u, ui) in m.media_urls" :key="ui" :href="u" target="_blank" class="mr-2 text-blue-400 hover:underline">[media]</a>
                          <div v-if="m.content" class="mt-1">{{ (m.content || '').slice(0,200) }}{{ (m.content||'').length>200 ? '…' : '' }}</div>
                        </div>
                        <div v-else-if="m.role === 'tool'">
                          {{ (m.content || '').slice(0,200) }}{{ (m.content||'').length>200 ? '…' : '' }}
                        </div>
                        <div v-else>
                          {{ (m.content || '').slice(0,200) }}{{ (m.content||'').length>200 ? '…' : '' }}
                        </div>
                      </div>
                      <pre v-else class="mt-2 overflow-x-auto bg-gray-900 p-2 text-[11px] text-green-300 whitespace-pre-wrap">{{ m.role === 'tool' ? tryPrettyJSON(m.content) : (m.content || '') }}</pre>
                    </div>
                  </div>
                </div>
              </div>
              <div v-else>(no output captured)</div>
            </div>
            <div v-else class="space-y-1">
              <div v-if="!jobLines.length" class="text-gray-500">No output captured.</div>
              <div v-else>
                <div v-for="(ln, idx) in jobLines" :key="idx" class="rounded-md border border-gray-800/30 bg-gray-950 p-2 font-mono text-[11px]">
                  <div class="mb-1 flex items-center justify-between text-xs text-gray-400">
                    <div class="text-gray-400">Line {{ idx + 1 }}</div>
                    <button class="text-[11px] text-blue-400 hover:text-blue-200" @click="toggleExpanded(selectedJob.id + '-line-' + idx)">
                      {{ isExpanded(selectedJob.id + '-line-' + idx) ? 'Hide' : 'Show' }}
                    </button>
                  </div>
                  <div class="whitespace-pre-wrap text-sm text-gray-200">
                    <div v-if="!isExpanded(selectedJob.id + '-line-' + idx)">
                      {{ (ln || '').slice(0,200) }}{{ (ln||'').length>200 ? '…' : '' }}
                    </div>
                    <pre v-else class="mt-2 overflow-x-auto bg-gray-900 p-2 text-[11px] text-green-300 whitespace-pre-wrap">{{ ln }}</pre>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
        <div v-else-if="selectedCompile" class="flex min-h-0 flex-1 flex-col overflow-y-auto px-5 py-3">
          <div class="mb-3 flex items-center justify-between">
            <p class="text-[10px] font-semibold uppercase tracking-wide text-gray-400">Compiler Stages</p>
            <button class="text-[10px] text-blue-500 hover:text-blue-700" @click="reloadCompile">
              ↺ Reload
            </button>
          </div>
          <div v-if="compileLoading" class="rounded-lg bg-gray-950 p-3 font-mono text-[11px] text-gray-500">
            Loading…
          </div>
          <div v-else-if="!selectedCompile.stages?.length"
            class="rounded-lg bg-gray-950 p-3 font-mono text-[11px] text-gray-500">
            No stage logs captured.
          </div>
          <div v-else class="space-y-3">
            <div v-for="(stage, index) in selectedCompile.stages" :key="`${stage.name}-${index}`"
              class="overflow-hidden rounded-lg border border-gray-200 dark:border-gray-800">
              <div class="flex items-center justify-between border-b border-gray-100 bg-gray-50 px-3 py-2 dark:border-gray-800 dark:bg-gray-950">
                <div>
                  <div class="text-xs font-semibold uppercase tracking-wide text-gray-600 dark:text-gray-300">
                    {{ stage.name }}
                  </div>
                  <div class="text-[10px] text-gray-400">
                    {{ fmtTs(stage.started_at) }}
                  </div>
                </div>
                <span class="inline-flex rounded-full px-2 py-0.5 text-[10px] font-semibold"
                  :class="stageStatusClass(stage.status)">
                  {{ stage.status }}
                </span>
              </div>
              <div class="space-y-3 px-3 py-3">
                <div v-if="stage.system_prompt">
                  <p class="mb-1 text-[10px] font-semibold uppercase tracking-wide text-gray-400">System</p>
                  <pre class="overflow-x-auto rounded bg-gray-950 p-3 font-mono text-[11px] text-gray-200 whitespace-pre-wrap">{{ stage.system_prompt }}</pre>
                </div>
                <div v-if="stage.user_prompt">
                  <p class="mb-1 text-[10px] font-semibold uppercase tracking-wide text-gray-400">User</p>
                  <pre class="overflow-x-auto rounded bg-gray-950 p-3 font-mono text-[11px] text-gray-200 whitespace-pre-wrap">{{ stage.user_prompt }}</pre>
                </div>
                <div v-if="stage.response">
                  <p class="mb-1 text-[10px] font-semibold uppercase tracking-wide text-gray-400">Response</p>
                  <pre class="overflow-x-auto rounded bg-gray-950 p-3 font-mono text-[11px] text-green-400 whitespace-pre-wrap">{{ stage.response }}</pre>
                </div>
                <div v-if="stage.error">
                  <p class="mb-1 text-[10px] font-semibold uppercase tracking-wide text-gray-400">Error</p>
                  <pre class="overflow-x-auto rounded bg-red-950/80 p-3 font-mono text-[11px] text-red-300 whitespace-pre-wrap">{{ stage.error }}</pre>
                </div>
              </div>
            </div>
            <div v-if="selectedCompile.script" class="overflow-hidden rounded-lg border border-gray-200 dark:border-gray-800">
              <div class="border-b border-gray-100 bg-gray-50 px-3 py-2 text-xs font-semibold uppercase tracking-wide text-gray-600 dark:border-gray-800 dark:bg-gray-950 dark:text-gray-300">
                Generated Script
              </div>
              <pre class="overflow-x-auto bg-gray-950 p-3 font-mono text-[11px] text-green-400 whitespace-pre-wrap">{{ selectedCompile.script }}</pre>
            </div>
          </div>
        </div>
      </div>
    </transition>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from "vue";
import {
	DETAILS_PANEL_MIN_WIDTH,
	useDetailsPanelLayout,
} from "../../composables/useDetailsPanelLayout";
import { useLogs } from "../../composables/useLogs";
import type { Job, SessionMessage, TaskCompile } from "../../stores/jobs";
import { formatDuration, jobDurationMs, useJobsStore } from "../../stores/jobs";
import { agentTaskType, useSettingsStore } from "../../stores/settings";
import { AppSelect } from "../ui/select";

const PAGE_SIZE_OPTIONS = [25, 50, 100].map((n) => ({
	value: n,
	label: String(n),
}));

// The Scheduled Tasks page's "More analytics": the agent's runs by day, its
// job queue and its compile attempts, whose rows open a detail drawer.
const props = defineProps<{
	/** The task (<agent>/<name>) whose runs the job queue shows; empty shows all. */
	task: string;
}>();
const emit = defineEmits<{ "update:task": [task: string] }>();

const store = useJobsStore();
const settings = useSettingsStore();
const { entries } = useLogs();
const detailsPanel = useDetailsPanelLayout();

const statusFilter = ref<string>("all");
const selectedJob = ref<Job | null>(null);
const selectedCompile = ref<TaskCompile | null>(null);
const jobOutput = ref<string>("");
const logsLoading = ref(false);
const sessionMessages = ref<SessionMessage[] | null>(null);
const sessionMessagesLoading = ref(false);
const expanded = ref<Record<string, boolean>>({});
const queueSection = ref<HTMLElement | null>(null);

function toggleExpanded(key: string) {
	expanded.value[key] = !expanded.value[key];
}

function isExpanded(key: string) {
	return !!expanded.value[key];
}

const jobLines = computed(() =>
	jobOutput.value ? jobOutput.value.split(/\r?\n/) : [],
);
const compileLoading = ref(false);
const liveBottom = ref<HTMLElement | null>(null);
const hoveredDay = ref<number | null>(null);
const runningJobID = ref<string | null>(null);
const stoppingJobID = ref<string | null>(null);
const jobsPage = ref(1);
const jobsPageSize = ref(25);

function tryPrettyJSON(s: unknown) {
	if (!s) return s || "";
	try {
		const v = typeof s === "string" ? JSON.parse(s) : s;
		return JSON.stringify(v, null, 2);
	} catch {
		return s;
	}
}

const statusFilters = [
	{ label: "All", value: "all" },
	{ label: "Pending", value: "pending" },
	{ label: "Running", value: "in_progress" },
	{ label: "Completed", value: "completed" },
	{ label: "Failed", value: "failed" },
];

const filteredJobs = computed(() =>
	store.jobs.filter(
		(j) =>
			(statusFilter.value === "all" || j.status === statusFilter.value) &&
			(!props.task || j.task_id === props.task),
	),
);
const totalJobPages = computed(() =>
	Math.max(1, Math.ceil(filteredJobs.value.length / jobsPageSize.value)),
);
const paginatedJobs = computed(() => {
	const start = (jobsPage.value - 1) * jobsPageSize.value;
	return filteredJobs.value.slice(start, start + jobsPageSize.value);
});
const jobsRangeLabel = computed(() => {
	if (!filteredJobs.value.length) return "Showing 0-0 of 0";
	const start = (jobsPage.value - 1) * jobsPageSize.value + 1;
	const end = Math.min(
		filteredJobs.value.length,
		start + paginatedJobs.value.length - 1,
	);
	return `Showing ${start}-${end} of ${filteredJobs.value.length}`;
});
const visibleTaskCompiles = computed(() => store.taskCompiles.slice(0, 5));
const selectedJobTask = computed(() => {
	const [agent, name] = (selectedJob.value?.task_id ?? "").split("/");
	return settings.config?.agents
		.find((a) => a.name === agent)
		?.tasks?.find((t) => t.name === name);
});
const selectedJobTaskType = computed(() =>
	agentTaskType(selectedJobTask.value?.type),
);
// The idle timeout the server applies to the selected run (default 15m);
// empty when it is disabled.
const selectedJobIdleTimeout = computed(() => {
	const value = selectedJobTask.value?.idle_timeout?.trim() || "15m";
	return /^0+(\.0*)?[a-zµ]*$/i.test(value) ? "" : value;
});

// The jobs list refreshes on its own; keep the open run current so it leaves
// Running when the run finishes, fails or times out.
watch(
	() => store.jobs,
	async (jobs) => {
		const open = selectedJob.value;
		if (!open) return;
		const latest = jobs.find((j) => j.id === open.id);
		if (!latest || latest === open) return;
		const finished =
			open.status === "in_progress" && latest.status !== "in_progress";
		selectedJob.value = latest;
		if (finished) await loadLogs(latest.id);
	},
);

// Live output: SSE log entries filtered by selected job's ID.
const liveLines = computed(() => {
	if (!selectedJob.value) return [];
	const id = selectedJob.value.id;
	return entries.value
		.filter((e) => e.attrs?.job_id === id)
		.map((e) => e.attrs?.chunk ?? e.msg);
});

watch(liveLines, async () => {
	if (selectedJob.value?.status === "in_progress") {
		await nextTick();
		liveBottom.value?.scrollIntoView({ behavior: "smooth" });
	}
});

watch([statusFilter, jobsPageSize, () => props.task], () => {
	jobsPage.value = 1;
});

// Picking a task's "View runs" brings its runs into view.
watch(
	() => props.task,
	async (task) => {
		if (!task) return;
		await nextTick();
		queueSection.value?.scrollIntoView({ block: "start", behavior: "smooth" });
	},
	{ immediate: true },
);

watch(
	() => filteredJobs.value.length,
	() => {
		if (jobsPage.value > totalJobPages.value) {
			jobsPage.value = totalJobPages.value;
		}
	},
);

async function selectJob(job: Job) {
	selectedCompile.value = null;
	selectedJob.value = job;
	jobOutput.value = "";
	if (job.status !== "in_progress") {
		// load persisted job output first
		await loadLogs(job.id);
		// if no explicit job output, try loading structured session messages inline
		if (!jobOutput.value && job.session_id) {
			sessionMessagesLoading.value = true;
			sessionMessages.value = await store.fetchSessionMessages(
				job.agent_id,
				job.session_id,
			);
			sessionMessagesLoading.value = false;
		} else {
			sessionMessages.value = null;
		}
	}
}

async function selectCompile(compile: TaskCompile) {
	selectedJob.value = null;
	selectedCompile.value = compile;
	compileLoading.value = true;
	try {
		selectedCompile.value =
			(await store.fetchTaskCompile(compile.id)) ?? compile;
	} finally {
		compileLoading.value = false;
	}
}

async function loadLogs(id: string) {
	logsLoading.value = true;
	jobOutput.value = await store.fetchLogs(id);
	logsLoading.value = false;
}

async function reloadLogs() {
	if (selectedJob.value) await loadLogs(selectedJob.value.id);
}

async function reloadCompile() {
	if (!selectedCompile.value) return;
	await selectCompile(selectedCompile.value);
}

function closeSelection() {
	selectedJob.value = null;
	selectedCompile.value = null;
}

async function runQueuedJobNow(jobID: string) {
	runningJobID.value = jobID;
	try {
		await store.runJobNow(jobID);
	} finally {
		runningJobID.value = null;
	}
}

async function stopJob(jobID: string) {
	stoppingJobID.value = jobID;
	try {
		await store.stopJob(jobID);
	} finally {
		stoppingJobID.value = null;
	}
}

function compileStatusLabel(status: TaskCompile["status"]): string {
	return (
		{
			succeeded: "Compiled",
			skipped: "Skipped",
			failed: "Failed",
		}[status] ?? status
	);
}

function compileStatusClass(status: TaskCompile["status"]): string {
	return (
		{
			succeeded:
				"bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300",
			skipped:
				"bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300",
			failed: "bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300",
		}[status] ?? "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300"
	);
}

function stageStatusClass(status: string): string {
	return (
		{
			succeeded:
				"bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300",
			failed: "bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300",
			started:
				"bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300",
		}[status] ?? "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300"
	);
}

function statusLabel(s: string): string {
	return (
		{
			pending: "Pending",
			in_progress: "Running",
			completed: "Done",
			failed: "Failed",
		}[s] ?? s
	);
}

function statusClass(s: string): string {
	return (
		{
			pending:
				"bg-yellow-100 text-yellow-700 dark:bg-yellow-900/40 dark:text-yellow-400",
			in_progress:
				"bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-400",
			completed:
				"bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-400",
			failed: "bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-400",
		}[s] ?? "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400"
	);
}

function fmtTs(ts: string): string {
	if (!ts) return "—";
	const d = new Date(ts);
	return `${d.toLocaleDateString(undefined, { month: "short", day: "numeric" })} ${d.toLocaleTimeString(
		undefined,
		{ hour: "2-digit", minute: "2-digit", second: "2-digit" },
	)}`;
}

function duration(job: Job): string {
	return formatDuration(jobDurationMs(job));
}

function dayH(
	row: { completed: number; failed: number; running: number },
	field: "completed" | "failed" | "running",
): number {
	const maxTotal = Math.max(
		...store.byDay.map((r) => r.completed + r.failed + r.running),
		1,
	);
	const total = row.completed + row.failed + row.running;
	if (!total) return 0;
	const totalH = Math.max(2, Math.round((total / maxTotal) * 60));
	const completedH = Math.round((row.completed / total) * totalH);
	const failedH = Math.round((row.failed / total) * totalH);
	const runningH = totalH - completedH - failedH;
	return field === "completed"
		? completedH
		: field === "failed"
			? failedH
			: runningH;
}

function showLabel(i: number, len: number): boolean {
	if (len <= 8) return true;
	const step = Math.max(1, Math.ceil(len / 8));
	return i === 0 || i === len - 1 || i % step === 0;
}
</script>
