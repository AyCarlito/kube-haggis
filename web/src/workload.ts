import type { ResourceObject } from './types'

/** Explains the accepted scale input range before a workload review. */
export const WORKLOAD_REPLICA_VALIDATION_MESSAGE = 'Enter a whole number from 0 to 9,007,199,254,740,991.'

/** Returns the current workload replica value in its editable string form. */
export function workloadReplicaInput(object: ResourceObject): string {
  const spec = object.spec
  if (!spec || typeof spec !== 'object' || Array.isArray(spec)) return '1'
  const replicas = (spec as Record<string, unknown>).replicas
  return typeof replicas === 'number' && Number.isFinite(replicas) ? String(replicas) : '1'
}

/** Parses a raw replica input without accepting coercive or unsafe numeric forms. */
export function parseWorkloadReplicaInput(value: string): number | undefined {
  if (!/^[0-9]+$/.test(value)) return undefined
  const replicas = Number(value)
  return Number.isFinite(replicas) && Number.isSafeInteger(replicas) && replicas >= 0 ? replicas : undefined
}
