import { defineStore } from 'pinia'
import { ref } from 'vue'
import { useMCP } from '../composables/useMCP'

export interface Agent {
  id: string
  name: string
  model: string
  state: string
}

export const useAgentsStore = defineStore('agents', () => {
  const agents = ref<Agent[]>([])
  const loading = ref(false)
  const { callTool } = useMCP()

  async function fetchAgents() {
    loading.value = true
    try {
      const raw = await callTool('agent_list')
      agents.value = JSON.parse(raw) as Agent[]
    } catch {
      agents.value = []
    } finally {
      loading.value = false
    }
  }

  return { agents, loading, fetchAgents }
})
