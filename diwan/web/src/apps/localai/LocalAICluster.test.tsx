import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import LocalAICluster from './LocalAICluster'
import { DEFAULT_POLICY } from '../../lib/localai/client'

const mock = vi.hoisted(() => ({ bootstrap: vi.fn(), request: vi.fn() }))
vi.mock('../../lib/localai/client', async importOriginal => ({ ...await importOriginal<typeof import('../../lib/localai/client')>(), AIClient: class { bootstrap = mock.bootstrap; request = mock.request } }))
beforeEach(() => {
  mock.bootstrap.mockReset().mockResolvedValue({ enabled: false, endpoint: 'http://127.0.0.1:52415', policy: DEFAULT_POLICY, trusted: {}, jobs: [], hostOS: 'windows', hostArch: 'amd64', exoCommit: 'fixture' })
  mock.request.mockReset()
})
it('opens disabled without probing Exo or starting inference', async () => {
  render(<LocalAICluster />)
  await screen.findByText('Disabled')
  expect(screen.getByText('Off until you choose to connect')).toBeVisible()
  expect(mock.request).not.toHaveBeenCalled()
})
it('reports a missing service without enabling AI or fabricating devices', async () => {
  mock.request.mockRejectedValue(new Error('Exo unavailable'))
  render(<LocalAICluster />)
  await screen.findByText('Disabled')
  fireEvent.click(screen.getByRole('button', { name: 'Configure Exo' }))
  fireEvent.click(screen.getByRole('button', { name: 'Probe Exo service' }))
  await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Exo unavailable'))
  expect(screen.getByText('Disabled')).toBeVisible()
  expect(screen.queryByRole('button', { name: 'Enable Local AI' })).not.toBeInTheDocument()
})

it('does not offer an image-only model as working text chat', async () => {
  mock.bootstrap.mockResolvedValue({ enabled: true, endpoint: 'http://127.0.0.1:52415', policy: DEFAULT_POLICY, trusted: { node: true }, jobs: [], hostOS: 'windows', hostArch: 'amd64', exoCommit: 'fixture' })
  mock.request.mockResolvedValue({ gateway: 'fixture', nodes: [{ id: 'node', name: 'Fixture', os: 'fixture OS', chip: 'unknown', backends: [], availableRam: null, availableDisk: null, interfaces: [], lastSeen: null }], models: [{ id: 'text', name: 'Text fixture', tasks: ['TextGeneration'], storage_size_megabytes: 1 }, { id: 'image', name: 'Image fixture', tasks: ['TextToImage'], storage_size_megabytes: 1 }], instances: [{ id: 'a', model: 'text', nodes: ['node'], residentBytes: 1000 }, { id: 'b', model: 'image', nodes: ['node'], residentBytes: 1000 }], observedAt: new Date().toISOString() })
  render(<LocalAICluster />)
  await screen.findByText('Connected')
  fireEvent.click(screen.getByRole('tab', { name: 'Models' }))
  const choices = screen.getAllByRole('button', { name: 'Use in AI Chat' })
  expect(choices[0]).toBeEnabled()
  expect(choices[1]).toBeDisabled()
})
