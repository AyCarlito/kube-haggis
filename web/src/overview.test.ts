import type { ResourceDefinition } from './types'
import { overviewNavigationTargets, workloadNavigationTargets } from './overview'

/** Fails a focused overview navigation assertion with a readable message. */
function assertOverview(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Exercises exact overview routes, aggregate workload gating, and optional Jobs/CronJobs links. */
export function runOverviewModelTests(): void {
  const definitions: ResourceDefinition[] = [
    { group: '', version: 'v1', resource: 'nodes', kind: 'Node', namespaced: false, scope: 'Cluster', verbs: ['list'] },
    { group: '', version: 'v1', resource: 'pods', kind: 'Pod', namespaced: true, scope: 'Namespaced', verbs: ['list'] },
    { group: '', version: 'v1', resource: 'persistentvolumeclaims', kind: 'PersistentVolumeClaim', namespaced: true, scope: 'Namespaced', verbs: ['list'] },
    { group: '', version: 'v1', resource: 'namespaces', kind: 'Namespace', namespaced: false, scope: 'Cluster', verbs: ['list'] },
    { group: 'apps', version: 'v1', resource: 'deployments', kind: 'Deployment', namespaced: true, scope: 'Namespaced', verbs: ['list'] },
    { group: 'apps', version: 'v1', resource: 'statefulsets', kind: 'StatefulSet', namespaced: true, scope: 'Namespaced', verbs: ['list'] },
    { group: 'apps', version: 'v1', resource: 'daemonsets', kind: 'DaemonSet', namespaced: true, scope: 'Namespaced', verbs: ['list'] },
    { group: 'batch', version: 'v1', resource: 'jobs', kind: 'Job', namespaced: true, scope: 'Namespaced', verbs: ['list'] },
    { group: 'batch', version: 'v1', resource: 'cronjobs', kind: 'CronJob', namespaced: true, scope: 'Namespaced', verbs: ['list'] },
  ]
  const targets = overviewNavigationTargets(definitions)
  assertOverview(targets.nodes === '/resources/core/v1/nodes', 'Nodes overview target was not canonical')
  assertOverview(targets.pods === '/resources/core/v1/pods' && targets.podRestarts === targets.pods, 'Pod overview targets did not share the canonical Pod list')
  assertOverview(targets.storageClaims === '/resources/core/v1/persistentvolumeclaims' && targets.boundClaims === targets.storageClaims, 'PVC overview targets were not canonical')
  assertOverview(targets.namespaces === '/resources/core/v1/namespaces', 'Namespaces overview target was not canonical')
  assertOverview(targets.workloads === '/workloads', 'aggregate workload target was not enabled for all controller kinds')

  const workloadTargets = workloadNavigationTargets(definitions)
  assertOverview(workloadTargets.every((target) => Boolean(target.path)), 'discovered workload targets were not all exposed')
  assertOverview(workloadTargets[3].path === '/resources/group-batch/v1/jobs' && workloadTargets[4].path === '/resources/group-batch/v1/cronjobs', 'Jobs/CronJobs links did not retain their exact GVR')

  const incomplete = definitions.filter((definition) => definition.resource !== 'daemonsets')
  const incompleteTargets = overviewNavigationTargets(incomplete)
  assertOverview(incompleteTargets.workloads === undefined, 'aggregate workload target appeared without every represented controller kind')
  assertOverview(incompleteTargets.nodes === targets.nodes, 'unrelated stable overview target changed with partial workload discovery')
}

runOverviewModelTests()
