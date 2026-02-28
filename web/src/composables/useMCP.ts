import { useAuthStore } from '../stores/auth'

export interface MCPResult {
  content?: Array<{ type: string; text?: string }>
  isError?: boolean
}

export function useMCP() {
  const auth = useAuthStore()

  function headers(): HeadersInit {
    const tok = auth.getToken()
    return tok ? { Authorization: `Bearer ${tok}` } : {}
  }

  async function callTool(name: string, args?: Record<string, unknown>): Promise<string> {
    const res = await fetch('/mcp', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        ...headers(),
      },
      body: JSON.stringify({
        jsonrpc: '2.0',
        id: Date.now(),
        method: 'tools/call',
        params: { name, arguments: args ?? {} },
      }),
    })

    if (!res.ok) {
      throw new Error(`MCP error: ${res.status} ${res.statusText}`)
    }

    const data = await res.json() as { result?: MCPResult; error?: { message: string } }
    if (data.error) {
      throw new Error(data.error.message)
    }

    const content = data.result?.content ?? []
    return content
      .filter((c) => c.type === 'text')
      .map((c) => c.text ?? '')
      .join('')
  }

  return { callTool }
}
