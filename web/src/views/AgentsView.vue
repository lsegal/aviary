<template>
  <AppLayout>
    <div class="px-6 py-6">
      <div class="mb-6 flex items-center justify-between">
        <h2 class="text-xl font-bold text-white">Agents</h2>
        <button
          class="rounded-lg bg-gray-800 px-4 py-2 text-sm text-gray-300 hover:bg-gray-700"
          @click="store.fetchAgents()"
        >Refresh</button>
      </div>

      <div v-if="store.loading" class="text-gray-400 text-sm">Loading…</div>
      <div v-else-if="!store.agents.length" class="text-gray-500 text-sm">No agents configured.</div>
      <div v-else class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <div
          v-for="agent in store.agents"
          :key="agent.id"
          class="rounded-xl border border-gray-800 bg-gray-900 p-5"
        >
          <div class="mb-1 flex items-center gap-2">
            <span class="font-semibold text-white">{{ agent.name }}</span>
            <span
              :class="agent.state === 'idle' ? 'bg-green-900 text-green-300' : 'bg-yellow-900 text-yellow-300'"
              class="rounded-full px-2 py-0.5 text-xs font-medium"
            >{{ agent.state }}</span>
          </div>
          <p class="text-xs text-gray-400">{{ agent.model }}</p>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import AppLayout from '../components/AppLayout.vue'
import { useAgentsStore } from '../stores/agents'

const store = useAgentsStore()
onMounted(() => store.fetchAgents())
</script>
