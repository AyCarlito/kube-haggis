import type { CustomResourceCapabilities, ResourceSchemaFallback, ResourceSchemaResponse, ResourceSummary } from './types'

/** Returns the concise CRD version identity shown beside a custom-resource view. */
export function customResourceCapabilityLabel(capabilities: CustomResourceCapabilities): string {
  const served = capabilities.servedVersions.length ? capabilities.servedVersions.join(', ') : 'none reported'
  const preferred = capabilities.preferredVersion || 'not reported'
  const storage = capabilities.storageVersion || 'not reported'
  return `served ${served} / preferred ${preferred} / storage ${storage}`
}

/** Returns a safe display value for one backend-evaluated printer column. */
export function printerColumnDisplay(item: ResourceSummary, name: string): string {
  const value = item.printerColumns?.[name]
  return typeof value === 'string' && value !== '' ? value : '—'
}

/** Formats field-level schema fallbacks as a bounded operator-facing explanation. */
export function schemaFallbackSummary(fallbacks: ResourceSchemaFallback[], truncated = false): string {
  const paths = fallbacks.slice(0, 6).map((fallback) => `${fallback.path}: ${fallback.reason}`)
  if (truncated || fallbacks.length > paths.length) paths.push('additional unsupported fields are available in YAML')
  return paths.join(' / ')
}

/** Returns the read-only explanation required when a resource exposes status fields. */
export function schemaStatusNotice(response: ResourceSchemaResponse): string {
  return response.statusSubresource ? 'The status subresource is enabled; status fields are read-only in this form.' : 'Status fields are server-managed and read-only in this form.'
}
