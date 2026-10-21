import type { ResourceDefinition } from './types'
import { overviewEventDetailPath, overviewEventsPath, overviewMetricDestination, overviewNavigationTargets, workloadNavigationTargets } from './overview'

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

  const metricDefinitions: ResourceDefinition[] = [
    ...definitions,
    { group: 'metrics.k8s.io', version: 'v1beta1', resource: 'nodes', kind: 'NodeMetrics', namespaced: false, scope: 'Cluster', verbs: ['get'] },
    { group: 'metrics.k8s.io', version: 'v1beta1', resource: 'pods', kind: 'PodMetrics', namespaced: true, scope: 'Namespaced', verbs: ['get'] },
  ]
  const nodeDestination = overviewMetricDestination(metricDefinitions, 'node', 'worker-a')
  assertOverview(nodeDestination?.path === '/resources/core/v1/nodes/worker-a', 'Node metric destination did not use the exact core Node GVR')
  assertOverview(nodeDestination?.namespace === undefined, 'cluster-scoped Node metric destination unexpectedly adopted a namespace')
  const podDestination = overviewMetricDestination(metricDefinitions, 'pod', 'api-0', 'team-a')
  assertOverview(podDestination?.path === '/resources/core/v1/pods/api-0' && podDestination.namespace === 'team-a', 'Pod metric destination did not preserve its namespace or exact core GVR')
  assertOverview(overviewMetricDestination(metricDefinitions, 'pod', 'api-0') === undefined, 'Pod metric without a namespace produced an unsafe detail destination')
  assertOverview(overviewMetricDestination(metricDefinitions.filter((definition) => definition.group !== ''), 'node', 'worker-a') === undefined, 'Node metric destination fell back to a metrics API or short resource slug')
  assertOverview(overviewMetricDestination(metricDefinitions.filter((definition) => definition.group !== ''), 'pod', 'api-0', 'team-a') === undefined, 'Pod metric destination fell back to a metrics API or short resource slug')

  const eventDefinitions: ResourceDefinition[] = [
    ...definitions,
    { group: '', version: 'v1', resource: 'events', kind: 'Event', namespaced: true, scope: 'Namespaced', verbs: ['get', 'list'] },
    { group: 'events.k8s.io', version: 'v1', resource: 'events', kind: 'Event', namespaced: true, scope: 'Namespaced', verbs: ['get', 'list'] },
  ]
  assertOverview(overviewEventsPath(eventDefinitions) === '/resources/core/v1/events', 'overview Event list did not retain the exact core GVR')
  assertOverview(overviewEventDetailPath(eventDefinitions, { name: 'warning', namespace: 'team-a' }) === '/resources/core/v1/events/warning', 'overview Event detail did not retain the exact core GVR')
  assertOverview(overviewEventDetailPath(eventDefinitions, { name: 'warning' }) === undefined, 'Event detail route appeared without a namespace')
  assertOverview(overviewEventsPath(eventDefinitions.filter((definition) => definition.group !== '' || definition.resource !== 'events')) === undefined, 'overview Event navigation guessed a route without core Events discovery')

  const workloadTargets = workloadNavigationTargets(definitions)
  assertOverview(workloadTargets.every((target) => Boolean(target.path)), 'discovered workload targets were not all exposed')
  assertOverview(workloadTargets[3].path === '/resources/group-batch/v1/jobs' && workloadTargets[4].path === '/resources/group-batch/v1/cronjobs', 'Jobs/CronJobs links did not retain their exact GVR')

  const incomplete = definitions.filter((definition) => definition.resource !== 'daemonsets')
  const incompleteTargets = overviewNavigationTargets(incomplete)
  assertOverview(incompleteTargets.workloads === undefined, 'aggregate workload target appeared without every represented controller kind')
  assertOverview(incompleteTargets.nodes === targets.nodes, 'unrelated stable overview target changed with partial workload discovery')
}

runOverviewModelTests()
