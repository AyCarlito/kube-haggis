import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { ComputeReadPackView } from './computeReadPack'
import type { ComputeQuantityValue, ComputeReadPackData } from './types'

/** Fails a focused Compute panel assertion with a readable message. */
function assertCompute(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Creates one bounded Compute quantity value for component fixtures. */
function computeValue(state: ComputeQuantityValue['state'], value?: string): ComputeQuantityValue {
  return { state, value }
}

/** Creates a complete Compute panel fixture with explicit object and telemetry states. */
function computeData(kind: 'Node' | 'Pod', usageState: ComputeReadPackData['usage']['state']): ComputeReadPackData {
  const available = { cpu: computeValue('available', '500m'), memory: computeValue('available', '1Gi'), state: 'available' as const }
  const notApplicable = { cpu: computeValue('not-applicable'), memory: computeValue('not-applicable'), state: 'not-applicable' as const, message: 'Not defined for this resource.' }
  return {
    kind,
    name: kind === 'Node' ? 'worker-a' : 'api-0',
    namespace: kind === 'Node' ? undefined : 'team-a',
    requests: kind === 'Node' ? notApplicable : available,
    limits: kind === 'Node' ? notApplicable : available,
    capacity: kind === 'Node' ? available : notApplicable,
    allocatable: kind === 'Node' ? available : notApplicable,
    usage: {
      cpu: computeValue(usageState === 'available' || usageState === 'stale' ? 'available' : usageState, usageState === 'available' || usageState === 'stale' ? '250m' : undefined),
      memory: computeValue(usageState === 'available' || usageState === 'stale' ? 'available' : usageState, usageState === 'available' || usageState === 'stale' ? '512Mi' : undefined),
      state: usageState,
      message: usageState === 'permission-denied' ? 'Kubernetes RBAC denied metrics access.' : 'Telemetry state is explicit.',
      timestamp: '2026-10-20T10:05:00Z',
    },
  }
}

/** Exercises Compute headings, consistent quantity labels, exact identity, and telemetry states. */
export function runComputeReadPackComponentTests(): void {
  const podMarkup = renderToStaticMarkup(<ComputeReadPackView data={computeData('Pod', 'available')} />)
  assertCompute(podMarkup.includes('CPU and memory') && podMarkup.includes('Requests') && podMarkup.includes('Limits') && podMarkup.includes('Actual usage'), 'Compute table labels were not rendered')
  assertCompute(podMarkup.includes('Pod/api-0') && podMarkup.includes('team-a') && podMarkup.includes('500m') && podMarkup.includes('250m'), 'Pod Compute identity or values were not rendered')
  assertCompute(podMarkup.includes('Capacity') && podMarkup.includes('not applicable'), 'Pod capacity semantics were not explicit')

  const nodeMarkup = renderToStaticMarkup(<ComputeReadPackView data={computeData('Node', 'stale')} />)
  assertCompute(nodeMarkup.includes('Node/worker-a') && nodeMarkup.includes('cluster scope'), 'Node identity was not preserved')
  assertCompute(nodeMarkup.includes('Node-level requests or limits') && nodeMarkup.includes('Stale'), 'Node semantics or stale telemetry state was not visible')

  const deniedMarkup = renderToStaticMarkup(<ComputeReadPackView data={computeData('Pod', 'permission-denied')} />)
  assertCompute(deniedMarkup.includes('RBAC denied') && deniedMarkup.includes('2026-10-20T10:05:00Z'), 'permission-denied telemetry or sample timestamp was hidden')

  const alignedData = computeData('Pod', 'available')
  alignedData.requests = {
    cpu: computeValue('available', '24'),
    memory: computeValue('available', '24G'),
    state: 'available',
  }
  alignedData.usage.cpu = computeValue('available', '12.116325676')
  alignedData.usage.memory = computeValue('available', '2.794655744G')
  const alignedMarkup = renderToStaticMarkup(<ComputeReadPackView data={alignedData} />)
  assertCompute(alignedMarkup.includes('12.116325676') && alignedMarkup.includes('2.794655744G') && !alignedMarkup.includes('12116325676n') && !alignedMarkup.includes('2729156Ki'), 'actual usage did not render in the request units')
}

runComputeReadPackComponentTests()
