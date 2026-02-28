<template>
  <AppLayout>
    <div class="flex h-full flex-col">
      <!-- Agent picker -->
      <div class="border-b border-gray-800 px-6 py-3">
        <select
          v-model="selectedAgent"
          class="rounded-lg border border-gray-700 bg-gray-800 px-3 py-1.5 text-sm text-white"
          @change="agentsStore.fetchAgents()"
        >
          <option value="">Select agent…</option>
          <option v-for="a in agentsStore.agents" :key="a.id" :value="a.name">{{ a.name }}</option>
        </select>
      </div>

      <!-- Messages -->
      <div ref="messagesEl" class="flex-1 overflow-y-auto px-6 py-4 space-y-4">
        <div v-for="(msg, i) in messages" :key="i" :class="msg.role === 'user' ? 'text-right' : 'text-left'">
          <span
            :class="msg.role === 'user'
              ? 'inline-block rounded-xl bg-blue-600 px-4 py-2 text-sm text-white max-w-lg'
              : 'inline-block rounded-xl bg-gray-800 px-4 py-2 text-sm text-gray-100 max-w-2xl whitespace-pre-wrap'"
          >{{ msg.text }}</span>
        </div>
        <div v-if="streaming" class="text-left">
          <span class="inline-block animate-pulse rounded-xl bg-gray-800 px-4 py-2 text-sm text-gray-400">…</span>
        </div>
      </div>

      <!-- Input -->
      <form class="border-t border-gray-800 px-6 py-4 flex gap-3" @submit.prevent="send">
        <input
          v-model="input"
          type="text"
          :disabled="streaming || !selectedAgent"
          placeholder="Type a message…"
          class="flex-1 rounded-xl border border-gray-700 bg-gray-800 px-4 py-2.5 text-sm text-white placeholder-gray-500 focus:border-blue-500 focus:outline-none disabled:opacity-50"
        />
        <button
          type="submit"
          :disabled="streaming || !input.trim() || !selectedAgent"
          class="rounded-xl bg-blue-600 px-5 py-2.5 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-40"
        >
          Send
        </button>
      </form>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, nextTick, onMounted } from 'vue'
import AppLayout from '../components/AppLayout.vue'
import { useAgentsStore } from '../stores/agents'
import { useStream } from '../composables/useStream'

interface Message { role: 'user' | 'assistant'; text: string }

const agentsStore = useAgentsStore()
const { streaming, streamAgent } = useStream()
const selectedAgent = ref('')
const input = ref('')
const messages = ref<Message[]>([])
const messagesEl = ref<HTMLElement | null>(null)

onMounted(() => agentsStore.fetchAgents())

async function scrollBottom() {
  await nextTick()
  if (messagesEl.value) messagesEl.value.scrollTop = messagesEl.value.scrollHeight
}

async function send() {
  const text = input.value.trim()
  if (!text || !selectedAgent.value) return
  input.value = ''
  messages.value.push({ role: 'user', text })
  await scrollBottom()

  try {
    await streamAgent(selectedAgent.value, text, (reply) => {
      messages.value.push({ role: 'assistant', text: reply })
      scrollBottom()
    })
  } catch (e) {
    messages.value.push({ role: 'assistant', text: `Error: ${e instanceof Error ? e.message : String(e)}` })
  }
}
</script>
