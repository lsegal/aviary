import { ref } from 'vue'
import { useAuthStore } from '../stores/auth'

export function useStream() {
  const auth = useAuthStore()
  const streaming = ref(false)
  const error = ref<string | null>(null)

  /**
   * Streams an agent_run response by polling the MCP endpoint.
   * Since MCP over HTTP returns the full response, this collects the result.
   */
  async function streamAgent(agentName: string, message: string, onChunk: (text: string) => void): Promise<void> {
    streaming.value = true
    error.value = null

    try {
      const tok = auth.getToken()
      const res = await fetch('/mcp', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...(tok ? { Authorization: `Bearer ${tok}` } : {}),
        },
        body: JSON.stringify({
          jsonrpc: '2.0',
          id: Date.now(),
          method: 'tools/call',
          params: {
            name: 'agent_run',
            arguments: { name: agentName, message },
          },
        }),
      })

      if (!res.ok) {
        throw new Error(`HTTP ${res.status}`)
      }

      const data = await res.json() as { result?: { content: Array<{ type: string; text?: string }> }; error?: { message: string } }
      if (data.error) {
        throw new Error(data.error.message)
      }
      const text = (data.result?.content ?? [])
        .filter((c) => c.type === 'text')
        .map((c) => c.text ?? '')
        .join('')

      onChunk(text)
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
      throw e
    } finally {
      streaming.value = false
    }
  }

  return { streaming, error, streamAgent }
}
