// The AI adapter is always same-origin. Do not use document endpoint failover:
// a failed local inference must never send a prompt to a cloud API.
export interface Memory { inBytes: number }
export interface ComputeNode {
  id: string; name: string; os: string; chip: string; backends: string[] | null
  availableRam: Memory | null; availableDisk: Memory | null
  interfaces: { name: string; interfaceType: string }[] | null; lastSeen: string | null
}
export interface Model { id: string; name: string; storage_size_megabytes: number; tasks: string[] }
export interface Instance { id: string; model: string; nodes: string[]; residentBytes: number }
export interface Snapshot { gateway: string; nodes: ComputeNode[]; models: Model[]; instances: Instance[]; observedAt: string }
export interface Policy { concurrent: number; queued: number; timeoutSeconds: number; memoryBudgetMB: number; maxTokens: number }
export interface Job { id: string; model: string; state: string; started: string; finished?: string; error?: string }
export interface Status { enabled: boolean; endpoint: string; policy: Policy; trusted: Record<string, boolean>; jobs: Job[]; hostOS: string; hostArch: string; exoCommit: string }
export const DEFAULT_POLICY: Policy = { concurrent: 1, queued: 2, timeoutSeconds: 120, memoryBudgetMB: 8192, maxTokens: 512 }

async function failure(response: Response): Promise<Error> {
  try {
    const result = await response.json() as { error?: string }
    return new Error(result.error || `Local AI request failed (HTTP ${response.status}).`)
  } catch { return new Error('The Local AI adapter is unavailable. Use an updated local launcher; public/hosted listeners do not expose AI.') }
}

export class AIClient {
  private token = ''
  async request<T>(path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
    const response = await fetch(`/api/local-ai/${path}`, {
      method: body === undefined ? 'GET' : 'POST', mode: 'same-origin', credentials: 'same-origin', redirect: 'error', cache: 'no-store',
      headers: { 'X-LocalFirst-AI-Token': this.token, ...(body === undefined ? {} : { 'Content-Type': 'application/json' }) },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }), ...(signal ? { signal } : {}),
    })
    if (!response.ok) throw await failure(response)
    try { return await response.json() as T } catch { throw new Error('The Local AI adapter is missing from this launcher. Rebuild the offline package.') }
  }
  async bootstrap(): Promise<Status> {
    const result = await this.request<Status & { token: string }>('bootstrap')
    if (typeof result.token !== 'string' || typeof result.enabled !== 'boolean') throw new Error('Incompatible Local AI adapter.')
    this.token = result.token
    return result
  }
  async chat(model: string, prompt: string, maxTokens: number, signal: AbortSignal, onDelta: (text: string) => void): Promise<void> {
    const response = await fetch('/api/local-ai/chat', {
      method: 'POST', mode: 'same-origin', credentials: 'same-origin', redirect: 'error', cache: 'no-store', signal,
      headers: { 'Content-Type': 'application/json', 'X-LocalFirst-AI-Token': this.token },
      body: JSON.stringify({ model, messages: [{ role: 'user', content: prompt }], max_tokens: maxTokens, consent: true }),
    })
    if (!response.ok) throw await failure(response)
    if (!response.headers.get('Content-Type')?.startsWith('text/event-stream') || !response.body) throw new Error('Exo did not return a response stream.')
    const reader = response.body.getReader()
    const decoder = new TextDecoder()
    let buffer = '', finished = false
    try {
      while (!finished) {
        const chunk = await reader.read()
        if (chunk.done) break
        buffer += decoder.decode(chunk.value, { stream: true }).replace(/\r/g, '')
        if (buffer.length > 128 * 1024) throw new Error('Exo stream event is too large.')
        let boundary: number
        while ((boundary = buffer.indexOf('\n\n')) !== -1) {
          const event = buffer.slice(0, boundary); buffer = buffer.slice(boundary + 2)
          const data = event.split('\n').filter(line => line.startsWith('data:')).map(line => line.slice(5).trimStart()).join('\n')
          if (!data) continue
          if (data.trim() === '[DONE]') { finished = true; break }
          const value = JSON.parse(data) as { error?: { message?: string } | string; choices?: { delta?: { content?: string } }[] }
          if (value.error) throw new Error(typeof value.error === 'string' ? value.error : value.error.message || 'Exo inference failed.')
          for (const choice of value.choices || []) if (typeof choice.delta?.content === 'string') onDelta(choice.delta.content)
        }
      }
      if (!finished) throw new Error('Exo disconnected before completing the response.')
    } finally { await reader.cancel().catch(() => { /* Already closed after cancellation. */ }); reader.releaseLock() }
  }
}
