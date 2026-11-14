# Local AI Cluster setup, verification and limits

## Status

This is an optional Exo HTTP integration, disabled on every LOCALFIRST process restart. It is not an Exo distribution or a claim that this Windows machine can execute Exo.

The supported software slice is implemented. End-to-end acceptance is still open: no genuine model inference or real Exo cluster connection has been verified on supported hardware.

| Feature | Status | Evidence or boundary |
|---|---|---|
| Independent lazy workspace, disabled state and unavailable-service errors | WORKING | TypeScript, UI tests and browser checks recorded below |
| Exo metadata and streaming HTTP adapter | PARTIALLY WORKING | Pinned-source schema and HTTP contract fixtures; real Exo hardware still needed |
| Explicit gateway/node approval, local endpoint restrictions, queue/backpressure and cancellation | WORKING | Automated contract/security tests; these do not establish cryptographic Exo peer authentication |
| Genuine model output and multi-node inference | NOT TESTED | No supported Exo service/model available on the Windows development host |
| Hard CPU/GPU/memory quotas or Exo automatic restart | UNSUPPORTED | This adapter controls requests, not the independently owned Exo service |
| Native Windows/Android Exo compute, Bluetooth/WebRTC inference | UNSUPPORTED | Audited platform/API/transport boundary; existing document transports remain independent |
| Physical Bluetooth transfer, cross-device WebRTC and network performance under real inference load | NOT TESTED | Requires the real devices and inference host |
| Editor AI actions, model downloads/placement and remote cluster joins | UNSUPPORTED | Deliberately excluded until real core inference can be verified; never overwrite or send documents |

Audit, hardware compatibility, source/licensing findings and the pre-change baseline: [LOCAL-AI-AUDIT.md](LOCAL-AI-AUDIT.md).

## Supported Exo host

Audited Exo **0.3.70**, commit **a60b95698ca81737d367443674dd5563cbc9559b**. Prefer an Apple Silicon Mac meeting the pinned README requirements. Linux CPU/CUDA support is experimental here because upstream source extras and its platform roadmap differ. Native Windows is excluded by Exo's uv platform environments. Installing it into this Windows application would not create supported compute hardware.

On a supported machine, review the pinned upstream installation instructions and install its required Python 3.13, uv and Rust tooling. The following is a manual source setup, not a command LOCALFIRST executes:

```sh
git clone https://github.com/exo-explore/exo
cd exo
git checkout a60b95698ca81737d367443674dd5563cbc9559b
cd dashboard
npm install
npm run build
cd ..
uv sync --extra mlx
EXO_OFFLINE=true uv run exo
```

The `mlx` example is for Apple Silicon. See the pinned README for Linux-specific extras; do not install the Mac backend on Windows. These dependency installs require network access and may be large. This command starts no model. Provision a compatible, explicitly approved local model separately in Exo before offline inference. The operator must choose the model license, approve its weight download, and load an instance using Exo's own dashboard/API. LOCALFIRST never adds models, places instances, executes shell commands, or starts Exo.

Secure the Exo cluster network and HTTP gateway with the host firewall. Exo's API normally uses port 52415; do not expose it to the public internet. Discovery/cluster membership remains Exo's responsibility, not LOCALFIRST Bluetooth pairing. An approved ID is an admission choice, not a certificate or proof of a trusted remote operating system. Exo can change membership while a request runs, so use only a controlled cluster; this adapter cannot enforce Exo-side per-peer authorization after submission.

For a gateway on another machine, create an authenticated tunnel from the LOCALFIRST machine:

```powershell
ssh -N -L 127.0.0.1:52415:127.0.0.1:52415 YOUR_USER@YOUR_EXO_HOST
```

Verify the SSH host key before approving the connection. In LOCALFIRST use `http://127.0.0.1:52415`. A numeric IPv6 loopback endpoint with an explicit port is also accepted. LAN/public URLs, DNS names, embedded credentials, redirects, custom paths and proxy-environment forwarding are rejected. HTTP credentials/API keys are not supported by this slice; use a secured tunnel to the native gateway rather than weakening endpoint restrictions.

## Build and run LOCALFIRST

Use the existing Node.js/Go installation. No new application dependencies are added.

```powershell
cd diwan\web
npm ci --no-audit --no-fund  # Only when dependencies need installation
npm run build:offline
npm run package:offline
cd ..\..
.\Start-LocalFirst-Offline.cmd
```

The launcher selects the latest complete package and opens the existing `http://127.0.0.1:8765` origin through the cache-upgrade page. Existing browser documents remain in the same IndexedDB. Save open editor work and export a backup before replacing an app package. A first source build may need network access for dependencies; a complete packaged app does not need Node/Go or internet to run.

For the normal Go server, the existing `Build-Diwan.ps1` / `Start-Diwan.cmd` flow still works. The AI API mounts only when `server.addr` is an explicit loopback listener, such as `127.0.0.1:8080`. Its route stays behind the existing authentication middleware and additionally requires a local socket peer, loopback Host, same-origin request and an independent per-process AI token. Public/cloud listeners have no AI route. Do not reverse-proxy the local AI route onto a public origin.

## Enable, use and stop

1. Open **Local AI Cluster → Settings**. Opening the module only reads its local bootstrap; it does not contact Exo.
2. Enter the loopback endpoint and choose **Probe Exo service**. Unavailable or incompatible services report an error and do not enable AI.
3. Review the reported gateway ID. Set request concurrency (1–2), queue capacity (0–4), request duration (10–300 seconds), advisory model memory budget and output-token limit. Approve the gateway/network checkbox and choose **Enable Local AI**.
4. In **Devices**, inspect the actual reported node IDs, backend, RAM, disk and interfaces. Unknown CPU architecture/GPU/bandwidth is explicitly unknown. Approve each reported node individually. Approval is local consent, not OS or cryptographic pairing.
5. In **Models**, select a text-generation model already downloaded and loaded in Exo. The UI and backend reject models without Exo's `TextGeneration` capability; image-generation models are not offered as text chat. Chat also refuses models without a verified placement/resident estimate or with an estimate over the configured advisory budget. Exo can still reject a loading/incompatible model; its real error is shown.
6. In **AI Chat**, type a prompt and authorize sending it. Only that typed prompt is submitted; this slice sends no automatic conversation history and no document content. Streaming output is plain escaped text. Cancellation closes the upstream request.
7. Choose **Cancel generation** or cancel a job in **Activity** to stop a request. **Disable AI** cancels all active/queued work, closes idle gateway connections and forgets trust. Revoking a node's trust also cancels pending work. Disable/re-enable to change policy.

Restarting LOCALFIRST resets AI to disabled. Leaving its page cancels that page's generation and stops its health checks. When enabled and visible, the page refreshes metadata every 15 seconds. If a network failure occurs, no request retries to a cloud endpoint; restore the same service and refresh manually. Changing the gateway identity requires fresh approval.

The application does not own Exo's process. To stop manually started Exo, use Ctrl+C in its terminal (or the operator's existing service manager). Close the SSH tunnel with Ctrl+C in its terminal. LOCALFIRST's Disable button does not stop Exo, unload weights, kill another user's inference, or restart a failed Exo service.

## Resource and privacy limits

- Default: one active job, two queued requests, 120-second end-to-end limit including queue wait, 512 output tokens. Hard request caps: 32 KiB JSON, 16 KiB total prompt text, 8 KiB per message, 32 messages, 128 KiB SSE line, 2 MiB response, 8 MiB metadata, 64 recent jobs.
- The memory setting is **advisory admission only**: compare the whole model's Exo-reported resident estimate with the configured budget. It does not reserve RAM or include KV cache, activations, other clients or GPU working memory. It is not a hard process quota.
- No CPU/GPU quotas, tensor-network priorities or physical bandwidth controls are claimed. Configure an independently managed OS service/cgroup or equivalent on the inference machine before substantial load. The optional historical training-worker Job Object is not reused for Exo.
- The Go adapter performs only bounded HTTP I/O in separate request goroutines with independent admission control. It never enters editor/CRDT/Bluetooth critical paths. This prevents an unbounded local request queue, but it cannot guarantee system responsiveness if the operator runs unrestricted Exo on the same machine.
- Trust admission checks all reported nodes and model placements immediately before sending each prompt. It cannot prevent an externally managed Exo cluster from changing placement after that check. Exo node membership/network security must be separately controlled by its operator.
- Chats are not persisted by LOCALFIRST. Job records keep model ID, state and timestamps, not prompt/output text or upstream error bodies. Errors shown in the page may contain Exo diagnostics. Exo and device operators may have their own logging; review that before submitting private information.
- No Bluetooth advertisements or invitations are exchanged for AI. A future Bluetooth advertisement path must show: “Device discovered via Bluetooth. High-bandwidth connection required for distributed inference.” This release never presents Bluetooth discovery as AI transport.

## Verification commands

```powershell
cd diwan
go test -timeout 90s ./...
go test -v -timeout 30s ./internal/localai ./cmd/localfirst-offline
go vet ./internal/localai ./cmd/localfirst-offline
cd web
npm test -- --reporter=dot
npm run typecheck
npm run lint
npm run build:frontend
npm run build:offline
npm run test:e2e:offline
cd ..\..
node diwan/scripts/verify-local-ai.mjs
```

HTTP fixtures in `internal/localai/service_test.go` and frontend tests are labelled **contract fixtures**. They test schema, transport and failures, not model output or hardware. The app never includes a fixture provider or simulator. Actual inference needs the pinned supported service plus a loaded model; it remains NOT TESTED until that evidence exists.

### Results from this implementation

| Check | Observed result |
|---|---|
| Normal production frontend build | PASS |
| Offline production frontend build | PASS; 688 local application assets |
| Normal Go backend build and Windows offline package | PASS |
| TypeScript | PASS before and after the integration |
| Lint of new AI source/tests | PASS; the touched App.tsx retains its two existing hook warnings |
| New frontend tests | 7 PASS: independent same-origin token, SSE fragmentation/errors/disconnection, no cloud retry, disabled/unavailable UI and incompatible-model UI |
| Full frontend unit suite | 1450 PASS, 4 FAIL, matching the four original failures; baseline was 1443 PASS, 4 FAIL |
| New Go contract/security tests and offline helper tests | PASS; includes endpoint/origin/token boundaries, default-off, actual schema parsing, untrusted/changed/unsupported nodes and model tasks, memory admission, bounded queue, cancellation, deadline and upstream/truncated-stream errors |
| Full Go suite | All tested packages pass except the original Windows `backend/storage` POSIX-mode assertion; `go vet` passes for the new package and offline helper |
| Offline Chrome regression suite | **38 PASS**, zero failures/skips/flaky outcomes in the verified run; real editors/storage/Yjs, with native radio transports stubbed where stated in the original tests |
| Real packaged helper / Chrome desktop and mobile | PASS: lazy page, no AI request before opening it, disabled bootstrap only, absent Exo stays disabled, saved document remains editable and survives fully offline reload, vertical scrolling, mobile width, no page errors/external requests |
| Normal Go runtime with isolated fresh data | PASS: core files API 200, AI default disabled, AI status without token 403; with auth enabled, anonymous AI bootstrap 401 |
| Genuine Exo inference, multi-node hardware, CPU/RAM quotas and latency under real inference | NOT TESTED / not provided; supported host and loaded model still required |

The original four frontend failures are `manifest.test.ts`, `brandAssets.test.ts`, the old `OfficeShell.test.tsx` navigation wording, and the Sheets solo-presence label in `sheetsEditor.integration.test.tsx`. The unchanged Go failure is `TestLocalShareLinks_TokenNeverHitsDisk` expecting POSIX 0600 where Windows reports 0666. Repository-wide lint additionally reports **40 errors and 226 warnings in existing source/tests outside the new AI module**; it is not claimed clean. No new AI file appears in those lint diagnostics.

The isolated original commit was also built and browser-tested in `.tools/ai-baseline`. Its full run had 36 passes and two failures (spreadsheet UI timing and the obsolete `Open shared document` selector). Early feature runs exposed existing test timing assumptions around PDF recovery and editor focus. Three regression-test changes now wait for the real PDF renderer, focus the editor with `locator.press`, and select the existing `Open shared file` control. All original document-content, persistence, synchronization and recovery assertions remain. No editor, CRDT or Bluetooth implementation was changed to make these tests pass.

Raw local evidence is retained in ignored `.tools/`: `baseline-frontend.log`, `baseline-backend.log`, `baseline-typecheck.log`, `baseline-offline-e2e-full.log`, `ai-frontend-regression.log`, `ai-backend-regression.log`, `ai-backend-targeted.log`, `ai-go-vet.log`, `ai-full-lint.log`, `ai-offline-e2e-verified.log`, `normal-ai-smoke.json`, and `local-ai-browser-results.json`. Browser screenshots are `local-ai-desktop.png` and `local-ai-mobile.png`. HTTP fixtures and native transport stubs do not establish physical Bluetooth/WebRTC links or genuine model output. No actual inference throughput/latency comparison is claimed.

## Exact change inventory

All paths below are relative to `diwan/`:

| Files | Purpose |
|---|---|
| `internal/localai/exo.go` | Typed, bounded, loopback-only real Exo HTTP provider and pinned API commit |
| `internal/localai/service.go` | Default-off runtime, token/origin/peer restrictions, explicit approval, model/memory admission, bounded jobs/queue and cancellation |
| `internal/localai/service_test.go` | Clearly labelled HTTP contract/security/failure tests; no runtime simulator |
| `main.go`, `cmd/localfirst-offline/main.go` | Add independent AI routes; preserve normal auth and restrict normal-server mounting to explicit loopback listeners |
| `web/src/App.tsx`, `web/src/components/Layout.tsx` | One lazy route and navigation entry |
| `web/src/lib/localai/client.ts`, `web/src/lib/localai/client.test.ts` | Independent same-origin request/SSE client and contract tests |
| `web/src/apps/localai/LocalAICluster.tsx`, `web/src/apps/localai/LocalAICluster.test.tsx` | Devices, models, chat, cluster, activity and settings; honest disabled/unsupported/error states and UI tests |
| `web/e2e-offline/bluetooth-files.e2e.ts`, `web/e2e-offline/replica-migration.e2e.ts`, `web/e2e-offline/two-replicas.e2e.ts` | Five added/two replaced test lines for real renderer readiness, focused keyboard input and current control wording |
| `scripts/verify-local-ai.mjs` | Real packaged-app failure isolation and offline persistence verification in an isolated browser/profile/port |
| `scripts/package-offline.mjs` | Include both AI guides and an optional-AI notice in the existing portable package; build commands stay the same |
| `docs/LOCAL-AI-AUDIT.md`, `docs/LOCAL-AI-SETUP.md` | Architecture/source/platform/license audit and this setup/results/rollback guide |

No dependency manifests, document engines, document stores, CRDT algorithms, WebRTC transports, Bluetooth implementation, schema migrations, cloud deployments or remote refs were changed. The root launcher and browser-notice edits were already uncommitted before this feature task and are excluded from its commits.

## Rollback

The dedicated branch is `feature/local-ai-cluster`. No remote publication is required. Keep the previous package directory: the source launcher always picks the newest complete bundle, so explicitly run the older package's own launcher to roll back its executable and UI without changing the browser origin or document database. Save work first; old editor tabs can retain old code until reopened.

For a source rollback, review `git diff` and revert only the integration commit/files listed in the final change report. Do not use `git reset --hard`, delete browser storage, restore generated notices, or undo the pre-existing launcher fix. Rebuild the previous source and select its package. The feature adds no migration, database table or document field to undo.
