import type { RelatedResource, ResourceDefinition } from './types'
import { relatedResourceDetailPath } from './catalog'

/** Fails a focused relationship navigation assertion with a readable message. */
function assertRelated(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Exercises exact Event involved-object routing and unresolved-link suppression. */
export function runRelatedModelTests(): void {
  const podDefinition: ResourceDefinition = {
    group: '',
    version: 'v1',
    resource: 'pods',
    kind: 'Pod',
    namespaced: true,
    scope: 'Namespaced',
    verbs: ['get'],
  }
  const resolved: RelatedResource = {
    type: 'involved-object',
    source: 'involvedObject',
    state: 'resolved',
    definition: podDefinition,
    apiVersion: 'v1',
    kind: 'Pod',
    name: 'api-0',
    namespace: 'team-a',
    uid: 'pod-uid',
  }
  assertRelated(relatedResourceDetailPath(resolved) === '/resources/core/v1/pods/api-0', 'resolved Event target did not retain the exact core Pod GVR')
  assertRelated(relatedResourceDetailPath({ ...resolved, state: 'unresolved' }) === undefined, 'unresolved Event target produced a clickable route')
  assertRelated(relatedResourceDetailPath({ ...resolved, definition: undefined }) === undefined, 'Event target without discovery produced a clickable route')
}

runRelatedModelTests()
