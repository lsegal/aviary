import { defineStore } from 'pinia'
import { ref } from 'vue'
import { useMCP } from '../composables/useMCP'

export interface Job {
  id: string
  task_id: string
  agent_name: string
  status: string
  attempts: number
  created_at: string
  updated_at: string
}

export const useTasksStore = defineStore('tasks', () => {
  const jobs = ref<Job[]>([])
  const loading = ref(false)
  const { callTool } = useMCP()

  async function fetchJobs(taskID = '') {
    loading.value = true
    try {
      const raw = await callTool('job_list', taskID ? { task: taskID } : {})
      jobs.value = (JSON.parse(raw) as Job[] | null) ?? []
    } catch {
      jobs.value = []
    } finally {
      loading.value = false
    }
  }

  return { jobs, loading, fetchJobs }
})
