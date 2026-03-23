<template>
  <AppLayout>
    <div class="flex h-full flex-col overflow-hidden">

      <!-- Header + Filters -->
      <div class="flex flex-shrink-0 flex-wrap items-center gap-3 border-b border-gray-200 px-6 py-4 dark:border-gray-800">
        <h2 class="mr-2 text-xl font-bold text-gray-900 dark:text-white">Jobs</h2>
        <div class="flex overflow-hidden rounded-lg border border-gray-200 text-sm dark:border-gray-700">
          <button v-for="p in presets" :key="p.days"
            class="px-3 py-1.5 transition-colors"
            :class="activePreset === p.days
              ? 'bg-blue-600 text-white'
              : 'bg-white text-gray-600 hover:bg-gray-100 dark:bg-gray-900 dark:text-gray-400 dark:hover:bg-gray-800'"
            @click="applyPreset(p.days)">
            {{ p.label }}
          </button>
        </div>
        <div class="flex items-center gap-1 text-sm">
          <input v-model="store.startDate" type="date"
            class="rounded-lg border border-gray-200 bg-white px-2 py-1.5 text-gray-700 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-300"
            @change="activePreset = null; store.fetch()" />
          <span class="text-gray-400">to</span>
          <input v-model="store.endDate" type="date"
            class="rounded-lg border border-gray-200 bg-white px-2 py-1.5 text-gray-700 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-300"
            @change="activePreset = null; store.fetch()" />
        </div>
        <!-- Status filter -->
        <div class="flex overflow-hidden rounded-lg border border-gray-200 text-sm dark:border-gray-700">
          <button v-for="s in statusFilters" :key="s.value"
            class="px-3 py-1.5 transition-colors"
            :class="statusFilter === s.value
              ? 'bg-blue-600 text-white'
              : 'bg-white text-gray-600 hover:bg-gray-100 dark:bg-gray-900 dark:text-gray-400 dark:hover:bg-gray-800'"
            @click="statusFilter = s.value">
            {{ s.label }}
          </button>
        </div>
        <span class="text-sm text-gray-500 dark:text-gray-400">{{ filteredJobs.length }} jobs</span>
        <button
          class="ml-auto rounded-lg bg-blue-600 px-4 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
          :disabled="store.loading || store.tasksLoading" @click="store.refreshAll()">
          {{ store.loading ? "Loading…" : "Refresh" }}
        </button>
      </div>

      <div v-if="store.error"
        class="mx-6 mt-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-400">
        {{ store.error }}
      </div>

      <div class="flex flex-1 overflow-hidden">

        <!-- Left: main content -->
        <div class="flex flex-1 flex-col overflow-y-auto px-6 py-4">

          <!-- Stat cards -->
          <div class="mb-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
            <div class="flex flex-col rounded-xl border border-yellow-200 bg-yellow-50 p-4 dark:border-yellow-900/50 dark:bg-yellow-950/30">
              <span class="mb-1 text-[10px] font-semibold uppercase tracking-wide text-yellow-600 dark:text-yellow-500">Pending</span>
              <span class="text-2xl font-bold text-yellow-700 dark:text-yellow-400">{{ store.pending.length }}</span>
            </div>
            <div class="flex flex-col rounded-xl border border-blue-200 bg-blue-50 p-4 dark:border-blue-900/50 dark:bg-blue-950/30">
              <span class="mb-1 flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wide text-blue-600 dark:text-blue-400">
                <span v-if="store.running.length > 0" class="inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-blue-500" />
                Running
              </span>
              <span class="text-2xl font-bold text-blue-700 dark:text-blue-400">{{ store.running.length }}</span>
            </div>
            <div class="flex flex-col rounded-xl border border-green-200 bg-green-50 p-4 dark:border-green-900/50 dark:bg-green-950/30">
              <span class="mb-1 text-[10px] font-semibold uppercase tracking-wide text-green-600 dark:text-green-500">Completed</span>
              <span class="text-2xl font-bold text-green-700 dark:text-green-400">{{ store.completed.length }}</span>
            </div>
            <div class="flex flex-col rounded-xl border border-red-200 bg-red-50 p-4 dark:border-red-900/50 dark:bg-red-950/30">
              <span class="mb-1 text-[10px] font-semibold uppercase tracking-wide text-red-600 dark:text-red-500">Failed</span>
              <span class="text-2xl font-bold text-red-700 dark:text-red-400">{{ store.failed.length }}</span>
            </div>
          </div>

          <!-- Charts row -->
          <div class="mb-4 grid gap-4 xl:grid-cols-2">
            <!-- Jobs by day -->
            <div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
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

            <!-- By agent breakdown -->
            <div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
              <h3 class="mb-4 text-sm font-semibold text-gray-700 dark:text-gray-300">By Agent</h3>
              <p v-if="!store.byAgent.length" class="text-xs text-gray-400">No data</p>
              <div v-for="ag in store.byAgent" :key="ag.name" class="mb-3">
                <div class="mb-1 flex items-center justify-between text-xs">
                  <span class="max-w-[60%] truncate text-gray-600 dark:text-gray-400">{{ ag.name }}</span>
                  <span class="flex gap-2 font-mono text-gray-500">
                    <span class="text-green-600 dark:text-green-400">{{ ag.completed }}✓</span>
                    <span v-if="ag.failed" class="text-red-500">{{ ag.failed }}✗</span>
                  </span>
                </div>
                <div class="h-1.5 w-full overflow-hidden rounded-full bg-gray-100 dark:bg-gray-800">
                  <div class="h-full rounded-full bg-green-500 transition-all"
                    :style="{ width: pct(ag.completed, ag.total) }" />
                </div>
              </div>
            </div>
          </div>

          <div class="mb-4 rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900">
            <div class="flex items-center justify-between border-b border-gray-100 px-5 py-3 dark:border-gray-800">
              <h3 class="text-sm font-semibold text-gray-700 dark:text-gray-300">Scheduled Tasks</h3>
              <span class="text-xs text-gray-400">{{ store.scheduledTasks.length }} configured</span>
            </div>
            <div v-if="store.tasksLoading" class="px-5 py-6 text-sm text-gray-400">
              Loading scheduled tasks…
            </div>
            <div v-else-if="!store.scheduledTasks.length" class="px-5 py-6 text-sm text-gray-400">
              No scheduled tasks configured.
            </div>
            <div v-else class="overflow-x-auto">
              <table class="w-full text-left text-xs">
                <thead>
                  <tr class="border-b border-gray-100 text-gray-400 dark:border-gray-800">
                    <th class="px-5 py-2.5 font-medium">Task</th>
                    <th class="px-4 py-2.5 font-medium">Agent</th>
                    <th class="px-4 py-2.5 font-medium">Trigger Type</th>
                    <th class="px-4 py-2.5 font-medium">Trigger</th>
                    <th class="px-4 py-2.5 font-medium">Task Type</th>
                    <th class="px-4 py-2.5 font-medium">Content</th>
                    <th class="px-4 py-2.5 text-right font-medium">Action</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="task in store.scheduledTasks" :key="task.id"
                    class="border-b border-gray-50 text-gray-700 dark:border-gray-800 dark:text-gray-300">
                    <td class="px-5 py-2.5">
                      <code class="font-mono text-gray-600 dark:text-gray-400">{{ task.id }}</code>
                    </td>
                    <td class="px-4 py-2.5 text-gray-600 dark:text-gray-400">{{ task.agent_name }}</td>
                    <td class="px-4 py-2.5 uppercase text-[10px] font-semibold text-gray-500 dark:text-gray-400">
                      {{ task.trigger_type }}
                    </td>
                    <td class="px-4 py-2.5 font-mono text-[11px] text-gray-500 dark:text-gray-400">
                      {{ taskTrigger(task) }}
                    </td>
                    <td class="px-4 py-2.5 uppercase text-[10px] font-semibold text-gray-500 dark:text-gray-400">
                      {{ task.type || "prompt" }}
                    </td>
                    <td class="max-w-lg truncate px-4 py-2.5 text-gray-600 dark:text-gray-400" :title="taskBody(task)">
                      {{ taskBody(task) || "—" }}
                    </td>
                    <td class="px-4 py-2.5 text-right">
                      <button
                        class="rounded-lg bg-blue-600 px-3 py-1.5 text-[11px] font-medium text-white hover:bg-blue-700 disabled:opacity-50"
                        :disabled="runningTaskID === task.id"
                        @click.stop="runScheduledTaskNow(task.id)"
                      >
                        {{ runningTaskID === task.id ? "Running…" : "Run Now" }}
                      </button>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

          <div class="mb-4 rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900">
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
                    <th class="px-4 py-2.5 font-medium">Agent</th>
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
                    <td class="px-4 py-2.5 text-gray-600 dark:text-gray-400">{{ compile.agent_id }}</td>
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

          <!-- Jobs table -->
          <div class="rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900">
            <div class="flex items-center justify-between border-b border-gray-100 px-5 py-3 dark:border-gray-800">
              <h3 class="text-sm font-semibold text-gray-700 dark:text-gray-300">Job Queue</h3>
              <span class="text-xs text-gray-400">{{ filteredJobs.length }} shown</span>
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
                    <th class="px-4 py-2.5 font-medium">Agent</th>
                    <th class="px-4 py-2.5 font-medium">Created</th>
                    <th class="px-4 py-2.5 font-medium">Duration</th>
                    <th class="px-4 py-2.5 text-right font-medium">Attempts</th>
                    <th class="px-4 py-2.5 font-medium">Scheduled</th>
                    <th class="px-4 py-2.5 text-right font-medium">Action</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="job in filteredJobs" :key="job.id"
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
                    <td class="px-4 py-2.5 text-gray-600 dark:text-gray-400">{{ job.agent_id }}</td>
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
                        @click.stop="runQueuedJobNow(job.id)"
                      >
                        {{ runningJobID === job.id ? "Running…" : "Run Now" }}
                      </button>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>

        <!-- Right: job detail panel -->
        <transition
          enter-active-class="transition-all duration-200 ease-out"
          leave-active-class="transition-all duration-150 ease-in"
          enter-from-class="opacity-0 translate-x-4"
          leave-to-class="opacity-0 translate-x-4">
          <div v-if="selectedJob || selectedCompile"
            class="flex w-96 flex-shrink-0 flex-col border-l border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900">
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
              <p class="mb-1 text-[10px] font-semibold uppercase tracking-wide text-gray-400">Prompt</p>
              <p class="text-xs text-gray-600 dark:text-gray-300">{{ selectedJob.prompt }}</p>
            </div>
            <div v-else-if="selectedCompile" class="border-b border-gray-100 px-5 py-3 dark:border-gray-800">
              <p class="mb-1 text-[10px] font-semibold uppercase tracking-wide text-gray-400">Prompt</p>
              <p class="text-xs text-gray-600 dark:text-gray-300">{{ selectedCompile.prompt || "—" }}</p>
              <p v-if="selectedCompile.reason" class="mt-3 mb-1 text-[10px] font-semibold uppercase tracking-wide text-gray-400">Reason</p>
              <p v-if="selectedCompile.reason" class="text-xs text-gray-600 dark:text-gray-300">{{ selectedCompile.reason }}</p>
            </div>

            <!-- Output / logs -->
            <div v-if="selectedJob" class="flex flex-1 flex-col overflow-hidden px-5 py-3">
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
                <div v-if="!liveLines.length" class="text-gray-500">Waiting for output…</div>
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
                            <div v-if="m.media_url">
                              <a :href="m.media_url" target="_blank" class="text-blue-400 hover:underline">[media]</a>
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
            <div v-else-if="selectedCompile" class="flex flex-1 flex-col overflow-y-auto px-5 py-3">
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
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from "vue";
import AppLayout from "../components/AppLayout.vue";
import { useLogs } from "../composables/useLogs";
import type { Job, ScheduledTask, TaskCompile } from "../stores/jobs";
import { useJobsStore } from "../stores/jobs";

const store = useJobsStore();
const { entries } = useLogs();

const activePreset = ref<number | null>(7);
const statusFilter = ref<string>("all");
const selectedJob = ref<Job | null>(null);
const selectedCompile = ref<TaskCompile | null>(null);
const jobOutput = ref<string>("");
const logsLoading = ref(false);
const sessionMessages = ref<object[] | null>(null);
const sessionMessagesLoading = ref(false);
const expanded = ref<Record<string, boolean>>({});

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
const runningTaskID = ref<string | null>(null);
const runningJobID = ref<string | null>(null);

const presets = [
	{ label: "Today", days: 0 },
	{ label: "7d", days: 7 },
	{ label: "30d", days: 30 },
];

// helpers for UI
function tryPrettyJSON(s: unknown) {
	if (!s) return s || "";
	try {
		const v = typeof s === "string" ? JSON.parse(s) : s;
		return JSON.stringify(v, null, 2);
	} catch (e) {
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

const filteredJobs = computed(() => {
	if (statusFilter.value === "all") return store.jobs;
	return store.jobs.filter((j) => j.status === statusFilter.value);
});
const visibleTaskCompiles = computed(() => store.taskCompiles.slice(0, 5));

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

function applyPreset(days: number) {
	activePreset.value = days;
	store.setPreset(days);
}

async function runScheduledTaskNow(taskID: string) {
	runningTaskID.value = taskID;
	try {
		await store.runTaskNow(taskID);
	} finally {
		runningTaskID.value = null;
	}
}

async function runQueuedJobNow(jobID: string) {
	runningJobID.value = jobID;
	try {
		await store.runJobNow(jobID);
	} finally {
		runningJobID.value = null;
	}
}

function taskTrigger(task: ScheduledTask): string {
	const parts: string[] = [];
	if (task.schedule) parts.push(`schedule: ${task.schedule}`);
	if (task.watch) parts.push(`watch: ${task.watch}`);
	if (task.start_at) parts.push(`start_at: ${task.start_at}`);
	if (task.run_once) parts.push("run_once");
	return parts.length ? parts.join(" | ") : "—";
}

function taskBody(task: ScheduledTask): string {
	return task.type === "script" ? task.script || "" : task.prompt || "";
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

// ── Formatting ────────────────────────────────────────────────────────────────

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
	return (
		d.toLocaleDateString(undefined, { month: "short", day: "numeric" }) +
		" " +
		d.toLocaleTimeString(undefined, {
			hour: "2-digit",
			minute: "2-digit",
			second: "2-digit",
		})
	);
}

function duration(job: Job): string {
	const start = new Date(job.created_at).getTime();
	const end = new Date(job.updated_at).getTime();
	const ms = end - start;
	if (ms < 1000) return `${ms}ms`;
	if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
	return `${Math.floor(ms / 60_000)}m ${Math.floor((ms % 60_000) / 1000)}s`;
}

function pct(part: number, total: number): string {
	if (!total) return "0%";
	return `${Math.round((part / total) * 100)}%`;
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

onMounted(async () => {
	await store.refreshAll();
});
</script>
