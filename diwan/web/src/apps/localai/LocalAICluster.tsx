import { useCallback, useEffect, useRef, useState } from 'react'
import { Cpu, RefreshCw, ShieldCheck, Square, Zap } from 'lucide-react'
import { Button, Card, Input, Tabs } from '../../components/ui'
import { AIClient, DEFAULT_POLICY } from '../../lib/localai/client'
import type { ComputeNode, Policy, Snapshot, Status } from '../../lib/localai/client'

const sections = ['Devices', 'Models', 'AI Chat', 'Cluster', 'Activity', 'Settings']
const message = (error: unknown) => error instanceof Error ? error.message : 'Local AI could not complete the request.'
const size = (bytes?: number) => bytes && bytes > 0 ? `${(bytes / (1024 ** 3)).toFixed(2)} GiB` : 'Not reported'
const field = 'w-full rounded-lg border border-line bg-paper text-ink px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-accent'
function nodeState(node: ComputeNode, trusted: boolean): string {
  if (/windows|android/i.test(node.os)) return 'Unsupported Exo compute platform'
  if (node.lastSeen && Date.now() - Date.parse(node.lastSeen) > 60000) return 'Disconnected'
  if (!trusted) return 'Discovered · approval required'
  return node.backends?.length ? 'Trusted · backend reported' : 'Trusted · compute support unknown'
}

export default function LocalAICluster() {
  const [client] = useState(() => new AIClient())
  const [status, setStatus] = useState<Status | null>(null)
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null)
  const [candidate, setCandidate] = useState<Snapshot | null>(null)
  const [endpoint, setEndpoint] = useState('http://127.0.0.1:52415')
  const [policy, setPolicy] = useState<Policy>(DEFAULT_POLICY)
  const [tab, setTab] = useState('Devices')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [consent, setConsent] = useState(false)
  const [sendConsent, setSendConsent] = useState(false)
  const [model, setModel] = useState('')
  const [prompt, setPrompt] = useState('')
  const [answer, setAnswer] = useState('')
  const [running, setRunning] = useState(false)
  const [chatState, setChatState] = useState('')
  const request = useRef<AbortController | null>(null)

  const refresh = useCallback(async () => {
    const next = await client.request<Status>('status')
    setStatus(next)
    if (next.enabled) {
      try { setSnapshot(await client.request<Snapshot>('snapshot')) }
      catch (reason) { setSnapshot(null); throw reason }
    }
    else setSnapshot(null)
  }, [client])
  useEffect(() => {
    let alive = true
    void client.bootstrap().then(async next => {
      if (!alive) return
      setStatus(next); setEndpoint(next.endpoint); setPolicy(next.policy)
      if (next.enabled) {
        const current = await client.request<Snapshot>('snapshot')
        if (alive) setSnapshot(current)
      }
    }).catch(reason => { if (alive) setError(message(reason)) })
    return () => { alive = false; request.current?.abort() }
  }, [client])
  useEffect(() => {
    if (!status?.enabled) return
    // Health checks exist only while this page is open and enabled.
    const timer = window.setInterval(() => {
      if (document.visibilityState !== 'visible') return
      void refresh().then(() => setError('')).catch(reason => { setSnapshot(null); setError(message(reason)) })
    }, 15000)
    return () => window.clearInterval(timer)
  }, [status?.enabled, refresh])

  async function action(work: () => Promise<void>) {
    setBusy(true); setError('')
    try { await work() } catch (reason) { setError(message(reason)) } finally { setBusy(false) }
  }
  async function probe() {
    setCandidate(null); setConsent(false)
    await action(async () => { setCandidate(await client.request<Snapshot>('probe', { endpoint })) })
  }
  async function connect() {
    if (!candidate || !consent) return
    await action(async () => {
      await client.request('connect', { endpoint, gateway: candidate.gateway, consent: true, policy })
      setCandidate(null); setConsent(false); await refresh(); setTab('Devices')
    })
  }
  async function disable() {
    request.current?.abort()
    await action(async () => { await client.request('disable', {}); setCandidate(null); setSendConsent(false); await refresh() })
  }
  async function trust(node: ComputeNode, trusted: boolean) {
    await action(async () => { await client.request('trust', { node: node.id, trusted }); await refresh() })
  }
  async function send() {
    if (!status?.enabled || !sendConsent || !prompt.trim()) return
    const controller = new AbortController(); request.current = controller
    setAnswer(''); setError(''); setRunning(true); setChatState('Queued / connecting')
    try {
      await client.chat(model, prompt, status.policy.maxTokens, controller.signal, delta => { setAnswer(value => value + delta); setChatState('Running') })
      setChatState('Completed')
    } catch (reason) {
      if (controller.signal.aborted) setChatState('Cancelled')
      else { setChatState('Failed'); setError(message(reason)) }
    } finally {
      request.current = null; setRunning(false)
      try { await refresh() } catch (reason) { setError(message(reason)) }
    }
  }
  const allTrusted = !!snapshot?.nodes.length && snapshot.nodes.every(node => status?.trusted[node.id])
  const enabled = !!status?.enabled
  const currentModel = snapshot?.instances.filter(instance => instance.model === model) || []
  const active = status?.jobs.filter(job => !job.finished).length || 0
  const state = running ? 'Running' : busy ? 'Connecting' : !status ? 'Unavailable' : !enabled ? 'Disabled' : !snapshot ? 'Disconnected' : 'Connected'

  return <div data-testid="local-ai-workspace" className="flex-1 min-h-0 min-w-0 w-full overflow-y-auto"><div className="w-full min-w-0 max-w-6xl mx-auto px-5 sm:px-9 py-8 space-y-6">
    <div className="flex flex-wrap items-start justify-between gap-4">
      <div className="min-w-0"><div className="flex items-center gap-3"><span className="rounded-xl bg-accent-tint p-2.5 text-accent"><Cpu size={23} /></span><h1 className="text-2xl font-semibold tracking-tight text-ink">Local AI Cluster</h1></div><p className="text-sm text-ink-muted mt-3 max-w-2xl">Optional inference through your own Exo service. Your files and collaboration stay independent.</p></div>
      <div className="flex items-center gap-3"><span role="status" className="text-xs font-medium rounded-full border border-line bg-paper px-3 py-1.5">{state}</span>{enabled && <Button variant="destructive" onClick={() => void disable()} disabled={busy}>Disable AI</Button>}</div>
    </div>
    {error && <div role="alert" className="rounded-xl border border-danger/30 bg-danger-bg text-danger px-4 py-3 text-sm break-words">{error}</div>}
    {!enabled && <Card className="p-6"><div className="flex gap-3"><ShieldCheck className="text-accent shrink-0" size={21} /><div><h2 className="font-semibold text-ink">Off until you choose to connect</h2><p className="text-sm text-ink-muted mt-2">No inference, model downloads or Exo background processes start with LOCALFIRST. Connect an existing service in Settings to begin.</p><Button className="mt-4" onClick={() => setTab('Settings')}>Configure Exo</Button></div></div></Card>}
    {status?.hostOS === 'windows' && <p className="text-xs leading-relaxed text-ink-muted border-l-2 border-accent pl-3">Native Windows Exo compute is unsupported by the audited upstream version. This Windows workspace can connect to an Exo gateway on supported hardware through a local SSH tunnel. Host: {status.hostOS}/{status.hostArch}.</p>}
    <div className="min-w-0 w-full overflow-x-auto"><Tabs className="min-w-max" value={tab} onChange={setTab} items={sections.map(label => ({ value: label, label }))} /></div>

    {tab === 'Devices' && <section aria-label="Compute devices" className="space-y-4">
      <div className="flex items-center justify-between gap-4"><div><h2 className="font-semibold text-ink">Devices reported by Exo</h2><p className="text-xs text-ink-muted mt-1">Discovery is read-only. Approve every node before sending a prompt. No remote code or OS pairing runs here.</p></div><Button onClick={() => void action(refresh)} disabled={!enabled || busy}><RefreshCw size={14} />Refresh</Button></div>
      {!snapshot?.nodes.length && <Card className="p-8 text-center text-sm text-ink-muted">{enabled ? 'No verified device snapshot. Check the Exo service and refresh.' : 'No devices discovered. AI is disabled.'}</Card>}
      {snapshot?.nodes.map(node => <Card key={node.id} className="p-5 space-y-4">
        <div className="flex flex-wrap justify-between gap-3"><div><h3 className="font-semibold text-ink">{node.name || node.id}</h3><p className="text-xs text-ink-muted mt-1">{nodeState(node, !!status?.trusted[node.id])}</p><p className="font-mono text-[11px] text-ink-faint mt-1 break-all">{node.id}</p></div><Button disabled={busy} onClick={() => void trust(node, !status?.trusted[node.id])}>{status?.trusted[node.id] ? 'Revoke trust' : 'Approve this node'}</Button></div>
        <dl className="grid sm:grid-cols-3 gap-4 text-xs">{[
          ['Operating system', node.os || 'Not reported'], ['Chip / accelerator identity', node.chip || 'Not reported'], ['CPU architecture', 'Not reported by this Exo API'],
          ['Available RAM', size(node.availableRam?.inBytes)], ['Available model disk', size(node.availableDisk?.inBytes)], ['Inference backends', node.backends?.join(', ') || 'Not reported'],
          ['Network interfaces', node.interfaces?.map(item => `${item.name}: ${item.interfaceType}`).join(', ') || 'Not reported'], ['Exo presence', 'Reported by gateway; not independently probed'], ['Last seen', node.lastSeen ? new Date(node.lastSeen).toLocaleString() : 'Not reported'],
        ].map(([label, value]) => <div key={label}><dt className="text-ink-faint">{label}</dt><dd className="text-ink mt-1 break-words">{value}</dd></div>)}</dl>
      </Card>)}
      <p className="text-xs text-ink-muted">Revoking trust cancels AI jobs. Removing a physical device from Exo requires Exo's own controls. Bluetooth discovery and document pairing are independent; Bluetooth-only AI inference is unsupported.</p>
    </section>}

    {tab === 'Models' && <section aria-label="Downloaded models" className="space-y-4"><h2 className="font-semibold text-ink">Downloaded models</h2><p className="text-sm text-ink-muted">Only models reported as downloaded by Exo appear here. Downloading and loading weights must be done explicitly in Exo; this workspace never triggers either.</p>
      {!snapshot?.models.length && <Card className="p-8 text-center text-sm text-ink-muted">No downloaded model information available.</Card>}
      {snapshot?.models.map(item => {
        const instances = snapshot.instances.filter(instance => instance.model === item.id)
        const textModel = (item.tasks || []).includes('TextGeneration')
        return <Card key={item.id} className="p-5"><h3 className="font-medium text-ink break-all">{item.name || item.id}</h3><p className="text-xs text-ink-muted mt-2">Download size: {item.storage_size_megabytes > 0 ? `${item.storage_size_megabytes} MiB (Exo estimate)` : 'Not reported'} · Resident estimate: {size(instances[0]?.residentBytes)}</p><p className="text-xs text-ink-muted mt-2">Tasks: {item.tasks?.join(', ') || 'Not reported'} · {textModel ? 'Text chat supported by model card' : 'Unsupported in text-only AI Chat'}</p><p className="text-xs text-ink-muted mt-2">{instances.length ? 'Instance present; readiness is validated by Exo on request' : 'No active instance. Load it in Exo first.'}</p><Button className="mt-4" disabled={!textModel || !instances.length || !allTrusted} onClick={() => { setModel(item.id); setTab('AI Chat') }}>Use in AI Chat</Button></Card>
      })}</section>}

    {tab === 'AI Chat' && <section aria-label="AI Chat" className="space-y-4"><h2 className="font-semibold text-ink">Ask your cluster</h2><p className="text-sm text-ink-muted">Only the text you submit below is sent. Chats stay in this page's memory and are cleared when you leave. No document is read or changed.</p>
      <label className="block text-xs text-ink-muted">Downloaded model<select aria-label="AI model" value={model} onChange={event => setModel(event.target.value)} className={`${field} mt-2`} disabled={running}><option value="">Select a text-generation model</option>{snapshot?.models.filter(item => (item.tasks || []).includes('TextGeneration')).map(item => <option key={item.id} value={item.id}>{item.name || item.id}</option>)}</select></label>
      <p className="text-xs text-ink-muted">{currentModel.length ? currentModel.map(instance => `${instance.nodes.length === 1 ? 'Single-device placement' : `${instance.nodes.length}-node distributed placement`} · resident estimate ${size(instance.residentBytes)}`).join('; ') : 'No active placement selected. Load a model in Exo first.'}</p>
      <label className="block text-xs text-ink-muted">Prompt<textarea aria-label="AI prompt" value={prompt} onChange={event => setPrompt(event.target.value)} maxLength={8192} rows={5} disabled={running} className={`${field} mt-2 resize-y`} placeholder="Write a prompt for your approved compute devices…" /></label>
      <label className="flex gap-2 items-start text-xs text-ink-muted"><input type="checkbox" checked={sendConsent} onChange={event => setSendConsent(event.target.checked)} />I authorize sending this prompt to this Exo cluster. Its operator and compute devices can read it.</label>
      {!allTrusted && <p className="text-xs text-ink-muted">Approve every reported device in Devices before submitting a prompt.</p>}
      <div className="flex items-center gap-3"><Button variant="primary" onClick={() => void send()} disabled={!enabled || !allTrusted || !model || !currentModel.length || !sendConsent || !prompt.trim() || running}><Zap size={14} />Send prompt</Button>{running && <Button onClick={() => request.current?.abort()}><Square size={13} />Cancel generation</Button>}<span className="text-xs text-ink-muted" aria-live="polite">{chatState}</span></div>
      {answer && <Card className="p-5"><h3 className="text-xs font-semibold text-ink-muted mb-3">Response</h3><pre className="whitespace-pre-wrap break-words font-sans text-sm text-ink leading-relaxed">{answer}</pre></Card>}
    </section>}

    {tab === 'Cluster' && <section className="space-y-4" aria-label="Cluster diagnostics"><h2 className="font-semibold text-ink">Verified observations</h2><Card className="p-5 space-y-3 text-sm text-ink-muted"><p>Gateway: <span className="font-mono break-all">{snapshot?.gateway || 'Not connected'}</span></p><p>Last snapshot: {snapshot ? new Date(snapshot.observedAt).toLocaleString() : 'None'}</p><p>Observed nodes: {snapshot?.nodes.length ?? 'Unknown'} · Active LOCALFIRST jobs: {active}</p><p>Model placements: {snapshot?.instances.length ?? 'Unknown'}</p><p>Compute transport: Exo-native IP / supported MLX transport. The API gateway uses loopback HTTP, optionally through an operator-managed SSH tunnel. Interface availability does not prove which physical link carries tensors.</p><p>Bluetooth data/control and WebRTC data channels remain document transports. No AI weights, tensors or advertisements use them.</p><p>No measured bandwidth, token speed, GPU utilization or hard CPU/memory isolation is available from this adapter.</p></Card></section>}

    {tab === 'Activity' && <section aria-label="AI activity" className="space-y-4"><div className="flex justify-between"><h2 className="font-semibold text-ink">Inference activity</h2><Button disabled={!status || busy} onClick={() => void action(refresh)}>Refresh activity</Button></div><p className="text-xs text-ink-muted">Up to 64 recent jobs in process memory. Prompts and generated text are excluded. A restart clears activity and disables AI.</p>{!status?.jobs.length && <Card className="p-8 text-center text-sm text-ink-muted">No inference jobs submitted.</Card>}{status?.jobs.slice().reverse().map(job => <Card key={job.id} className="p-4 flex flex-wrap justify-between gap-3"><div><p className="text-sm text-ink break-all">{job.model}</p><p className="text-xs text-ink-muted mt-1">{job.state} · {new Date(job.started).toLocaleTimeString()}</p>{job.error && <p className="text-xs text-danger mt-2 break-words">{job.error}</p>}</div>{!job.finished && <Button onClick={() => void action(async () => { await client.request('cancel', { id: job.id }); await refresh() })}>Cancel job</Button>}</Card>)}</section>}

    {tab === 'Settings' && <section aria-label="AI settings" className="space-y-5"><h2 className="font-semibold text-ink">Exo connection &amp; resource policy</h2><Card className="p-5 space-y-5">
      <Input label="Local Exo endpoint" value={endpoint} disabled={enabled || busy} onChange={event => { setEndpoint(event.target.value); setCandidate(null); setConsent(false) }} hint="Numeric loopback HTTP only. For another machine, create an authenticated SSH tunnel first." />
      <div className="grid sm:grid-cols-2 gap-4">{([
        ['concurrent', 'Maximum concurrent jobs', 1, 2], ['queued', 'Maximum queued requests', 0, 4], ['timeoutSeconds', 'Request time limit (seconds)', 10, 300], ['memoryBudgetMB', 'Advisory model memory budget (MiB)', 128, 1048576], ['maxTokens', 'Maximum output tokens per request', 16, 2048],
      ] as const).map(([key, label, min, max]) => <Input key={key} label={label} type="number" min={min} max={max} value={policy[key]} disabled={enabled || busy} onChange={event => setPolicy(value => ({ ...value, [key]: Number(event.target.value) }))} />)}</div>
      <p className="text-xs leading-relaxed text-ink-muted">These are request admission limits. The memory budget compares Exo's model-resident estimate, excluding KV cache, activations and other jobs. Hard CPU/GPU and memory limits are not provided. Manage the separate Exo process with your operating system. Disable AI before editing settings.</p>
      {!enabled && <Button onClick={() => void probe()} disabled={!status || busy}>{busy ? 'Checking Exo…' : 'Probe Exo service'}</Button>}
      {candidate && !enabled && <div className="space-y-3 p-4 rounded-lg bg-bg-elev2"><p className="text-xs text-ink-muted break-all">Discovered gateway: {candidate.gateway}. {candidate.nodes.length} nodes reported. This is discovery, not trusted pairing.</p><label className="flex gap-2 items-start text-xs text-ink-muted"><input type="checkbox" checked={consent} onChange={event => setConsent(event.target.checked)} />I approve this gateway and its separately secured Exo network. I will approve individual nodes before sending prompts.</label><Button variant="primary" disabled={!consent || busy} onClick={() => void connect()}>Enable Local AI</Button></div>}
    </Card><Card className="p-5 text-xs text-ink-muted space-y-3"><p>Backend: Exo 0.3.70 API, audited commit <code className="break-all">{status?.exoCommit || 'a60b95698ca81737d367443674dd5563cbc9559b'}</code>.</p><p>Native Windows and Android compute, Bluetooth inference, browser-native Exo execution, automatic model management and remote cluster joins are unsupported here. No cloud fallback is configured.</p><p>If Exo is absent or crashes, use your editors normally. Refresh to reconnect after restoring the same gateway; a changed gateway identity requires disabling and approving it again.</p><p>Stopping this workspace's jobs does not stop Exo or other applications using it. AI restarts disabled and forgets all trusted devices.</p></Card></section>}
  </div></div>
}
