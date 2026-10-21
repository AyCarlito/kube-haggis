import type { ResourceDefinition } from './types'

/** Converts a backend read-pack state into concise operator-facing copy. */
export function operationalReadPackStateLabel(state: string): string {
  if (state === 'permission-denied') return 'RBAC denied'
  if (state === 'available') return 'Available'
  if (state === 'empty') return 'Empty'
  if (state === 'partial') return 'Partial'
  if (state === 'unavailable') return 'Unavailable'
  if (state === 'error') return 'Read error'
  return 'Unknown'
}

/** Returns true for exact built-in resource families with an F5 structured read pack. */
export function isOperationalReadPackResource(definition: ResourceDefinition): boolean {
  if (definition.group === 'networking.k8s.io' && ['ingresses', 'networkpolicies'].includes(definition.resource)) return true
  if (definition.group === 'discovery.k8s.io' && definition.resource === 'endpointslices') return true
  if (definition.group === 'autoscaling' && definition.resource === 'horizontalpodautoscalers') return true
  if (definition.group === 'policy' && definition.resource === 'poddisruptionbudgets') return true
  if (definition.group === 'storage.k8s.io' && definition.resource === 'storageclasses') return true
  if (definition.group === '' && ['pods', 'resourcequotas', 'limitranges', 'persistentvolumes', 'serviceaccounts'].includes(definition.resource)) return true
  return false
}
