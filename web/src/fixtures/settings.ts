import type { EnvironmentReport, Finding } from '../runtime/environment'
import { harnesses, harnessNames } from '../settings/contracts'
import type {
  Binding,
  Harness,
  ModelDiscovery,
  ModelRef,
  Preferences,
  SettingsState,
  TestRun,
  TestStatus,
} from '../settings/contracts'

const available = (detail: string): Finding => ({
  status: 'available',
  detail: 'Fixture only: ' + detail,
})
const unknown = (detail: string): Finding => ({ status: 'unknown', detail })
export const settingsScenarios = [
  'first-launch',
  'saved',
  'changed-default',
  'no-harness',
  'unknown-models',
  'unsupported-override',
  'test-pending',
  'test-running',
  'test-succeeded',
  'test-failed',
  'test-cancelled',
  'test-interrupted',
] as const
export type SettingsScenario = (typeof settingsScenarios)[number]

export function settingsFixture(scenario: SettingsScenario) {
  const defaults: Record<Harness, ModelRef> = {
    opencode: { providerID: 'fixture-provider', id: 'fixture-default-a' },
    codex: { id: 'fixture-codex-default' },
    claude: { id: 'fixture-claude-canonical-a' },
  }
  const models = Object.fromEntries(
    harnesses.map((h) => [
      h,
      {
        harness: h,
        catalog: available('deterministic native catalog, not installed/provider metadata.'),
        configuredDefault: available(
          'configured/default fixture model; new sessions must still resolve and pin it.',
        ),
        defaultModel: defaults[h],
        models: [
          defaults[h],
          h === 'opencode'
            ? { providerID: 'fixture-provider', id: 'fixture-model-b' }
            : { id: `fixture/${h}-native-id` },
        ],
        explicitSelection: available(
          'native identifiers may be entered in this fixture; no provider configuration is changed.',
        ),
      },
    ]),
  ) as Record<Harness, ModelDiscovery>
  if (scenario === 'unknown-models' || scenario === 'unsupported-override') {
    models.opencode = {
      ...models.opencode,
      defaultModel: undefined,
      models: [],
      configuredDefault: unknown(
        'Configured/default model cannot be discovered safely in this fixture state.',
      ),
      catalog: unknown(
        'Catalog unknown, not empty. No credential inspection, service startup or model request occurred.',
      ),
    }
  }
  if (scenario === 'unsupported-override')
    models.opencode.explicitSelection = {
      status: 'unsupported',
      detail:
        'Fixture adapter cannot validate this existing explicit selection. It is preserved, not replaced with a default.',
    }
  if (scenario === 'changed-default') {
    const changed = { providerID: 'fixture-provider', id: 'fixture-default-after-change' }
    models.opencode.defaultModel = changed
    models.opencode.models.push(changed)
  }
  const preferences: Preferences = {
    schemaVersion: 1,
    setupConfirmed: false,
    defaultHarness: '',
    models: {
      opencode: { mode: 'default' },
      codex: { mode: 'default' },
      claude: { mode: 'default' },
    },
  }
  if (scenario !== 'first-launch' && scenario !== 'no-harness')
    preferences.defaultHarness = 'opencode'
  if (scenario === 'saved' || scenario === 'unsupported-override') {
    preferences.setupConfirmed = true
    preferences.models.opencode = {
      mode: 'explicit',
      model: { providerID: 'fixture-provider', id: 'fixture-model-b' },
    }
  }
  if (scenario === 'changed-default') preferences.setupConfirmed = true
  const state: SettingsState = {
    preferences,
    persistence: available(
      'preview confirmation changes fixture memory only, never runtime/disk settings.',
    ),
    testAnswer: available('test states are simulated; no inference or provider quota is used.'),
  }
  const environment: EnvironmentReport = {
    tools: harnesses.map((h) => ({
      id: h,
      name: harnessNames[h],
      optional: false,
      status: scenario === 'no-harness' ? 'missing' : 'available',
      version: 'fixture-1',
      detail: 'Deterministic installation fixture.',
      guidance: 'Use external harness setup; never enter provider credentials here.',
      documentation: 'https://example.invalid/fixture-only',
      capabilities: {
        protocol: available('native protocol'),
        service: available('simulated service'),
        policy: available('simulated policy'),
        authentication: unknown('Login not certified.'),
        entitlement: unknown('Quota not certified.'),
        compatibility:
          scenario === 'no-harness'
            ? { status: 'missing', detail: 'No installed harness in this fixture.' }
            : available('simulated compatible adapter, not a production claim.'),
      },
    })),
  }
  const binding: Binding = {
    harness: 'opencode',
    harnessVersion: 'fixture-1',
    protocol: 'fixture-native',
    nativeSessionID: 'fixture-existing-session',
    workspaceID: 'fixture-workspace',
    requested: { mode: 'default' },
    effective: { ...defaults.opencode },
  }
  const status = scenario.startsWith('test-') ? (scenario.slice(5) as TestStatus) : undefined
  const testRun: TestRun | undefined = status
    ? {
        id: 'fixture-test',
        clientRequestID: 'fixture-request',
        purpose: 'environment_test',
        status,
        harness: 'opencode',
        selection: { mode: 'default' },
        binding:
          status === 'pending'
            ? undefined
            : { ...binding, nativeSessionID: 'fixture-independent-test-session' },
        error:
          status === 'failed'
            ? 'Fixture model unavailable; no harness/model/provider fallback was attempted.'
            : undefined,
      }
    : undefined
  return { state, models, environment, binding, testRun }
}
