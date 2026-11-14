# Local AI Cluster architecture audit

Audit date: 14 November 2026. Implementation starts after this audit.

## Source baseline

- LOCALFIRST checkout: `d60bfee61b8241df875c1afe901a88e9df6e098d`, branch `feature/local-ai-cluster`.
- The local origin is `Vasu-Gupta105/BIOS2026-LOCALFIRST`; the requested LOCALFIRST URL is a separate repository. This task changes the user's existing checkout, without changing its remote or publishing anything.
- Existing uncommitted launcher and generated browser-notice changes are retained.
- Exo: **0.3.70**, commit **a60b95698ca81737d367443674dd5563cbc9559b**. A read-only audit clone lives in ignored `.tools/exo-audit`; it is not an application dependency.

## Existing systems and boundaries

| System | Actual implementation | Integration rule |
|---|---|---|
| React routing and navigation | `web/src/App.tsx`, `components/Layout.tsx`; lazy editor engines | Add one lazy workspace route and navigation entry |
| Documents and other editors | `web/src/apps/{docs,sheets,slides,whiteboard,pdf}` | No changes to editor engines, undo history or content |
| Offline persistence | `lib/offline/workspace.ts`, IndexedDB, committed revisions and backups | AI stores no documents and never reads this database |
| CRDT and P2P | `lib/crdt/yP2PSession.ts`, Yjs state-vector recovery, encrypted WebRTC room frames; `backend/rendezvous` only introduces peers | Do not import or modify the transport or CRDT modules |
| Bluetooth | `internal/nearby`, Windows paired classic RFCOMM, pinned TLS, invitation approval; browser `lib/nearby/nativeBridge.ts` | Independent service and session; do not reuse its token, service UUID or invitations |
| Experimental training | `internal/computeexp`, build-tagged companion and separate Bluetooth service | Not Exo, not distributed inference; leave unchanged |
| Go server | `main.go`, Gin authenticated route groups, local/hosted modes | Keep existing middleware; additionally restrict AI to a local socket peer and loopback Host |
| Offline helper | `cmd/localfirst-offline`, port 8765, read-only bundled assets plus separate nearby mux | Mount isolated `/api/local-ai/`; offline CSP already allows same-origin requests |
| Build and deployment | Vite, Go, `scripts/build-offline.mjs`, `package-offline.mjs`, container/cloud scripts | Add no runtime dependency, database, automatic sidecar launch or deployment requirement |

The offline generic document API is intercepted by `lib/offline/localApi.ts`. The AI client must use its own same-origin fetch; it must never use the document endpoint failover or send prompts to a cloud endpoint. Existing service workers already bypass `/api/`.

## Exo source findings

Pinned source links:

- [README](https://github.com/exo-explore/exo/blob/a60b95698ca81737d367443674dd5563cbc9559b/README.md)
- [Platform roadmap](https://github.com/exo-explore/exo/blob/a60b95698ca81737d367443674dd5563cbc9559b/PLATFORMS.md)
- [API](https://github.com/exo-explore/exo/blob/a60b95698ca81737d367443674dd5563cbc9559b/docs/api.md)
- [Dependencies](https://github.com/exo-explore/exo/blob/a60b95698ca81737d367443674dd5563cbc9559b/pyproject.toml)
- [State schema](https://github.com/exo-explore/exo/blob/a60b95698ca81737d367443674dd5563cbc9559b/src/exo/shared/types/state.py)
- [API implementation](https://github.com/exo-explore/exo/blob/a60b95698ca81737d367443674dd5563cbc9559b/src/exo/api/main.py)

Exo is an independent Python 3.13/Rust service with MLX inference backends. Its HTTP API normally uses port 52415. Nodes discover one another through Exo's IP networking (Rust multicast/Zenoh); inference uses its own MLX ring/JACCL mechanisms, including supported Thunderbolt/RDMA configurations. A browser cannot execute this native engine. HTTP integration requires no Exo patch or source vendoring.

`GET /node_id` identifies the configured gateway. `GET /state` returns camel-case node identities, memory, disk, backends, instances and runners. Instances are tagged unions, not a flat list. `GET /models?status=downloaded` returns an OpenAI-style `{data: [...]}` envelope, despite the API document's shorthand description as an array. Model memory comes from the instance's model card, not a guessed parameter count. Streaming uses `POST /v1/chat/completions`; closing the HTTP connection cancels a generation. A request without a pre-existing instance returns 404; it need not trigger model placement or downloads.

Exo's HTTP gateway and cluster membership are not a replacement for LOCALFIRST authentication or Bluetooth pairing. LOCALFIRST will not expose an arbitrary Exo proxy, shell runner, download route, or cluster-control route. Remote gateways require an operator-created authenticated SSH tunnel to a numeric loopback address. Exo's own peer security and model execution remain a separate operator responsibility.

## Compatibility matrix

| Platform or transport | Upstream evidence | This integration |
|---|---|---|
| Apple Silicon macOS | Tier 1; MLX Metal; README requires macOS 15.1+ | Eligible if the actual service reports compatible nodes; hardware verification still required |
| Linux CPU | Source extras and README startup exist; roadmap still labels planned | Experimental upstream; do not certify without a real node test |
| Linux CUDA | Source extras exist, README/roadmap support claims differ | Not certified here; report actual node backend and test on the target hardware |
| Native Windows CPU/CUDA | Longer-term roadmap; uv environments exclude Windows | Unsupported Exo compute host; LOCALFIRST UI/adapter can run on Windows |
| Android | No compatible native node demonstrated in pinned source | Unsupported compute host |
| Bluetooth RFCOMM | LOCALFIRST document transport only | Existing document sharing preserved; Exo inference unsupported |
| WebRTC data channel | LOCALFIRST collaboration only | Not an Exo compute transport |
| Wi-Fi/Ethernet | Exo IP transport; reported interfaces available | Independent of collaboration; no promise about physical-link throughput |
| Thunderbolt/RDMA | Supported upstream on specific Apple hardware and OS | Report only Exo evidence; not available on this Windows test host |

Do not display discovered devices as trusted or ready. User consent approves the gateway and individually approves visible compute node IDs. Each inference rechecks gateway identity, current model membership and the entire active compute-node set. A new or removed node invalidates admission until refreshed and approved. Node IDs are not cryptographic authentication: secure the tunnel and Exo network separately.

## Smallest supported slice

Add an optional Local AI Cluster page, a same-origin token-protected Go adapter, explicit probe/enable/trust/disable controls, real Exo state and downloaded-model queries, streamed chat with disconnect cancellation, bounded concurrency/queue/prompt/output/runtime, and an advisory model-resident-memory admission budget. Keep activity in bounded process memory and prompts only in the page's memory. No polls or inference processes run while disabled. Disabling cancels active and queued requests and forgets trust.

CPU/GPU hard limits, sidecar process restart, new model downloads/placement, remote cluster joins and OS pairing are **not** safely provided by these HTTP APIs. The UI must explain these limits and direct operators to manage the independent service. Do not claim application-level queue limits are OS isolation. No optional editor actions will be added before real core inference is verified.

## Available verification and blockers

On this Windows host there is no Exo service on 127.0.0.1:52415. A supported Exo host and an already-loaded model are required for genuine inference and multi-node performance tests. Never use an HTTP test fixture as proof of inference. Contract fixtures may test parsing, stream forwarding, cancellation, admission and failure isolation, clearly labelled as fixtures.

Baseline commands and captured logs (ignored `.tools/`):

- `npm test -- --reporter=dot`: 1443 passed, **4 failed**, 120 files. Pre-existing failures: manifest name, brand asset consistency, old OfficeShell navigation expectations, Sheets solo-presence label. See `baseline-frontend.log`.
- `go test ./...`: all tested packages pass except `backend/storage` `TestLocalShareLinks_TokenNeverHitsDisk`, which expects POSIX 0600 while Windows reports 0666. See `baseline-backend.log`.
- TypeScript baseline is recorded separately before implementation.
- Offline frontend build and packaged helper already ran successfully before this task; browser document save and fully offline reload passed in an isolated profile.

Post-implementation results and operational instructions belong in `LOCAL-AI-SETUP.md`. Physical Bluetooth, real cross-device WebRTC, actual Exo inference, model correctness and bandwidth/latency under GPU load require their respective hardware and cannot be inferred from unit tests.

## Licenses

LOCALFIRST retains its MIT/Apache-2.0 licensing. Exo is Apache-2.0. This change communicates over HTTP and neither copies nor redistributes Exo source, Python/Rust dependencies or model weights. Operators installing Exo must retain its license/NOTICE obligations and review the licenses of their chosen model, MLX, Rust and Python distributions. The existing LOCALFIRST third-party-notice build remains in place. No new npm/Go dependencies are planned.
