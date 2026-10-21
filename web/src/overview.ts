import { catalogEventResourcePath, catalogResourcePath } from './catalog'
import type { EventSummary, ResourceDefinition } from './types'

/** Describes one exact workload resource destination shown by the workload hub. */
export interface WorkloadNavigationTarget {
  key: string
  label: string
  description: string
  path?: string
}

/** Describes the stable resource and aggregate destinations available from the overview. */
export interface OverviewNavigationTargets {
  nodes?: string
  pods?: string
  workloads?: string
  storageClaims?: string
  namespaces?: string
  boundClaims?: string
  podRestarts?: string
}

/** Returns the exact core/v1 Events list route when that resource is discoverable. */
export function overviewEventsPath(definitions: ResourceDefinition[]): string | undefined {
  const events = definitions.some((definition) => definition.group === '' && definition.version === 'v1' && definition.resource === 'events')
  return events ? catalogEventResourcePath(definitions) : undefined
}

/** Returns an exact core/v1 Event detail route only when its name and namespace are present. */
export function overviewEventDetailPath(definitions: ResourceDefinition[], event: Pick<EventSummary, 'name' | 'namespace'>): string | undefined {
  if (!event.name || !event.namespace) return undefined
  return overviewEventsPath(definitions) ? catalogEventResourcePath(definitions, event.name) : undefined
}

/** Identifies the standard Kubernetes resource represented by one overview destination. */
interface OverviewResourceIdentity {
  group: string
  version: string
  resource: string
}

/** Defines the controller and task resources represented by the workload navigation hub. */
const workloadResourceIdentities: Array<OverviewResourceIdentity & Pick<WorkloadNavigationTarget, 'key' | 'label' | 'description'>> = [
  { key: 'deployments', label: 'Deployments', description: 'Replica-managed application controllers', group: 'apps', version: 'v1', resource: 'deployments' },
  { key: 'statefulsets', label: 'StatefulSets', description: 'Stable identity and storage controllers', group: 'apps', version: 'v1', resource: 'statefulsets' },
  { key: 'daemonsets', label: 'DaemonSets', description: 'One Pod on each eligible node', group: 'apps', version: 'v1', resource: 'daemonsets' },
  { key: 'jobs', label: 'Jobs', description: 'Finite task runs', group: 'batch', version: 'v1', resource: 'jobs' },
  { key: 'cronjobs', label: 'CronJobs', description: 'Scheduled task templates', group: 'batch', version: 'v1', resource: 'cronjobs' },
]

/** Finds a resource only when its complete discovered group, version, and resource identity matches. */
function findExactDefinition(definitions: ResourceDefinition[], identity: OverviewResourceIdentity): ResourceDefinition | undefined {
  return definitions.find((definition) => definition.group === identity.group && definition.version === identity.version && definition.resource === identity.resource)
}

/** Returns the canonical route for one exact discovered resource identity. */
function exactResourcePath(definitions: ResourceDefinition[], identity: OverviewResourceIdentity): string | undefined {
  const definition = findExactDefinition(definitions, identity)
  return definition ? catalogResourcePath(definition) : undefined
}

/** Builds workload links without selecting a misleading single controller kind for aggregate health. */
export function workloadNavigationTargets(definitions: ResourceDefinition[]): WorkloadNavigationTarget[] {
  return workloadResourceIdentities.map(({ key, label, description, group, version, resource }) => ({
    key,
    label,
    description,
    path: exactResourcePath(definitions, { group, version, resource }),
  }))
}

/** Builds exact overview destinations and exposes the aggregate workload hub only when its three controller kinds are discoverable. */
export function overviewNavigationTargets(definitions: ResourceDefinition[]): OverviewNavigationTargets {
  const nodes = exactResourcePath(definitions, { group: '', version: 'v1', resource: 'nodes' })
  const pods = exactResourcePath(definitions, { group: '', version: 'v1', resource: 'pods' })
  const storageClaims = exactResourcePath(definitions, { group: '', version: 'v1', resource: 'persistentvolumeclaims' })
  const namespaces = exactResourcePath(definitions, { group: '', version: 'v1', resource: 'namespaces' })
  const workloadTargets = workloadNavigationTargets(definitions)
  const controllerTargets = workloadTargets.slice(0, 3)

  return {
    nodes,
    pods,
    workloads: controllerTargets.every((target) => target.path) ? '/workloads' : undefined,
    storageClaims,
    namespaces,
    boundClaims: storageClaims,
    podRestarts: pods,
  }
}
