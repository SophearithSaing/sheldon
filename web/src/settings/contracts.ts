import type { Finding } from '../runtime/environment'

export type Harness = 'opencode' | 'codex' | 'claude'
export const harnessNames: Record<Harness, string> = {
  opencode: 'OpenCode',
  codex: 'Codex',
  claude: 'Claude Code',
}
export const harnesses: Harness[] = ['opencode', 'codex', 'claude']

export interface ModelRef {
  providerID?: string
  id: string
}
export type ModelSelection =
  { mode: 'default'; model?: never } | { mode: 'explicit'; model: ModelRef }
export interface Preferences {
  schemaVersion: 1
  setupConfirmed: boolean
  defaultHarness: Harness | ''
  models: Partial<Record<Harness, ModelSelection>>
  updatedAt?: string
}
export interface Confirmation {
  schemaVersion: 1
  confirm: true
  defaultHarness: Harness
  models: Preferences['models']
}
export interface SettingsState {
  preferences: Preferences
  persistence: Finding
  testAnswer: Finding
}
export interface ModelDiscovery {
  harness: Harness
  catalog: Finding
  configuredDefault: Finding
  defaultModel?: ModelRef
  models: ModelRef[]
  explicitSelection: Finding
}
export interface Binding {
  harness: Harness
  harnessVersion: string
  protocol: string
  nativeSessionID: string
  workspaceID: string
  requested: ModelSelection
  effective: ModelRef
}
export interface TestRequest {
  clientRequestID: string
  harness: Harness
  selection: ModelSelection
  confirmQuota: true
}
export type TestStatus =
  'pending' | 'running' | 'succeeded' | 'failed' | 'cancelled' | 'interrupted'
export interface TestRun {
  id: string
  clientRequestID: string
  purpose: 'environment_test'
  status: TestStatus
  harness: Harness
  selection: ModelSelection
  binding?: Binding
  error?: string
}

export function copyPreferences(p: Preferences): Preferences {
  return {
    ...p,
    models: Object.fromEntries(
      Object.entries(p.models).map(([h, s]) => [
        h,
        s.mode === 'explicit' ? { mode: 'explicit', model: { ...s.model } } : { mode: 'default' },
      ]),
    ),
  }
}

export function modelLabel(model?: ModelRef): string {
  if (!model) return 'Unknown — not resolved'
  return model.providerID ? `${model.providerID} · ${model.id}` : model.id
}

// Shape validation only. Real model existence, aliases and entitlement need a native adapter.
export function selectionError(h: Harness, selection: ModelSelection): string {
  if (selection.mode === 'default') return ''
  if (
    !/^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,199}$/.test(selection.model.id) ||
    selection.model.id.includes('://')
  )
    return 'Enter a native model ID (not a display name, URL with credentials, or API key).'
  if (
    h === 'opencode' &&
    !/^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(selection.model.providerID ?? '')
  )
    return 'OpenCode requires its existing native provider ID separately from the model ID.'
  if (h !== 'opencode' && selection.model.providerID)
    return 'This harness uses a native model ID, not an OpenCode provider/model pair.'
  return ''
}
