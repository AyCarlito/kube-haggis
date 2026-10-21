import { customResourceCapabilityLabel, printerColumnDisplay, schemaFallbackSummary, schemaStatusNotice } from './crd'
import type { ResourceSchemaResponse, ResourceSummary } from './types'

/** Fails a focused CRD model assertion with a readable message. */
function assertCRD(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Exercises version identity, safe list values, and schema fallback/read-only explanations. */
export function runCRDModelTests(): void {
  const capabilities = {
    group: 'widgets.example.com',
    version: 'v1beta1',
    kind: 'Widget',
    plural: 'widgets',
    resource: 'widgets',
    scope: 'Namespaced' as const,
    servedVersions: ['v1', 'v1beta1'],
    preferredVersion: 'v1beta1',
    storageVersion: 'v1',
    statusSubresource: true,
    scaleSubresource: true,
  }
  assertCRD(customResourceCapabilityLabel(capabilities).includes('served v1, v1beta1'), 'CRD served-version identity was not preserved')
  assertCRD(customResourceCapabilityLabel(capabilities).includes('preferred v1beta1 / storage v1'), 'CRD preferred/storage identity was not explained')

  const item: ResourceSummary = { name: 'blue', kind: 'Widget', uid: '', createdAt: '', status: '', resourceVersion: '7', printerColumns: { Phase: 'Ready' } }
  assertCRD(printerColumnDisplay(item, 'Phase') === 'Ready' && printerColumnDisplay(item, 'Missing') === '—', 'printer-column display did not use safe fallbacks')

  const response: ResourceSchemaResponse = {
    state: 'available',
    group: capabilities.group,
    version: capabilities.version,
    resource: capabilities.resource,
    fallbacks: [{ path: '$.spec.template', reason: 'unsupported schema construct "oneOf"' }],
    statusSubresource: true,
  }
  assertCRD(schemaFallbackSummary(response.fallbacks || []).includes('$.spec.template'), 'schema fallback path was not explained')
  assertCRD(schemaStatusNotice(response).includes('status fields are read-only'), 'status subresource read-only notice was not exposed')
}

runCRDModelTests()
