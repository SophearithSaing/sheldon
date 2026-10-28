export type DiagnosticStatus = 'available' | 'missing' | 'unsupported' | 'unknown'

export interface Finding {
  status: DiagnosticStatus
  detail: string
}

export interface EnvironmentTool {
  id: string
  name: string
  optional: boolean
  status: DiagnosticStatus
  version?: string
  detail: string
  guidance: string
  documentation: string
  capabilities?: {
    protocol: Finding
    service: Finding
    policy: Finding
    authentication: Finding
    entitlement: Finding
    compatibility: Finding
  }
}

export interface EnvironmentReport {
  tools: EnvironmentTool[]
}
