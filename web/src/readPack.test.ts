import { isOperationalReadPackResource, operationalReadPackStateLabel } from './readPack'
import type { ResourceDefinition } from './types'

/** Fails a focused read-pack model assertion with a readable message. */
function assertReadPack(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Exercises exact built-in family classification and explicit panel-state labels. */
export function runOperationalReadPackModelTests(): void {
  const pod: ResourceDefinition = { group: '', version: 'v1', resource: 'pods', kind: 'Pod', namespaced: true, scope: 'Namespaced', verbs: ['get', 'list'] }
  const customPod: ResourceDefinition = { group: 'example.com', version: 'v1', resource: 'pods', kind: 'Pod', namespaced: true, scope: 'Namespaced', verbs: ['get', 'list'] }
  assertReadPack(isOperationalReadPackResource(pod), 'Pod was not classified as an operational read-pack resource')
  assertReadPack(!isOperationalReadPackResource(customPod), 'custom resource was incorrectly classified as a built-in read-pack resource')
  assertReadPack(operationalReadPackStateLabel('permission-denied') === 'RBAC denied', 'permission state label was not explicit')
  assertReadPack(operationalReadPackStateLabel('partial') === 'Partial' && operationalReadPackStateLabel('unknown') === 'Unknown', 'read-pack state labels did not preserve safe fallbacks')
}

runOperationalReadPackModelTests()
