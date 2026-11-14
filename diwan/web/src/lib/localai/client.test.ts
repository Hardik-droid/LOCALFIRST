import { afterEach, describe, expect, it, vi } from 'vitest'
import { AIClient } from './client'

afterEach(() => vi.unstubAllGlobals())
const encoder = new TextEncoder()
function streamFixture(parts: string[]) {
  return new Response(new ReadableStream({ start(controller) { for (const part of parts) controller.enqueue(encoder.encode(part)); controller.close() } }), { headers: { 'Content-Type': 'text/event-stream' } })
}
describe('Local AI HTTP contract (fixtures, not model inference)', () => {
  it('uses only same-origin requests with the independent AI token', async () => {
    const fetch = vi.fn().mockResolvedValueOnce(Response.json({ token: 'ai-only', enabled: false })).mockResolvedValueOnce(Response.json({ enabled: false }))
    vi.stubGlobal('fetch', fetch)
    const client = new AIClient(); await client.bootstrap(); await client.request('status')
    expect(fetch.mock.calls[1][0]).toBe('/api/local-ai/status')
    expect(fetch.mock.calls[1][1]).toMatchObject({ mode: 'same-origin', redirect: 'error', cache: 'no-store', headers: { 'X-LocalFirst-AI-Token': 'ai-only' } })
  })
  it('parses fragmented SSE and rejects upstream error events', async () => {
    const fetch = vi.fn().mockResolvedValueOnce(streamFixture(['data: {"cho', 'ices":[{"delta":{"content":"fixture token"}}]}\r\n\r\n', 'data: [DONE]\n\n'])).mockResolvedValueOnce(streamFixture(['data: {"error":{"message":"model failed"}}\n\n']))
    vi.stubGlobal('fetch', fetch)
    const client = new AIClient(); const delta = vi.fn()
    await client.chat('fixture/model', 'typed prompt', 16, new AbortController().signal, delta)
    expect(delta).toHaveBeenCalledWith('fixture token')
    await expect(client.chat('fixture/model', 'typed prompt', 16, new AbortController().signal, delta)).rejects.toThrow('model failed')
  })
  it('does not mark truncated generation as complete', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(streamFixture(['data: {"choices":[]}\n\n'])))
    await expect(new AIClient().chat('fixture/model', 'prompt', 16, new AbortController().signal, vi.fn())).rejects.toThrow('disconnected')
  })
  it('does not retry unavailable inference through another endpoint', async () => {
    const fetch = vi.fn().mockRejectedValue(new TypeError('connection lost')); vi.stubGlobal('fetch', fetch)
    await expect(new AIClient().chat('fixture/model', 'prompt', 16, new AbortController().signal, vi.fn())).rejects.toThrow('connection lost')
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(fetch.mock.calls[0][0]).toBe('/api/local-ai/chat')
  })
})
