import { parseWorkloadReplicaInput, workloadReplicaInput } from './workload'
import type { ResourceObject } from './types'

/** Fails a focused workload scale-model assertion with a readable message. */
function assertWorkload(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Exercises initial zero preservation, raw editing, strict invalidation, and zero-to-one input. */
export function runWorkloadModelTests(): void {
  const zeroWorkload: ResourceObject = { spec: { replicas: 0 } }
  let editingValue = workloadReplicaInput(zeroWorkload)
  assertWorkload(editingValue === '0', 'initial zero was not preserved as editable text')

  editingValue = ''
  assertWorkload(editingValue === '' && parseWorkloadReplicaInput(editingValue) === undefined, 'deleting zero did not leave a blank invalid value')
  editingValue = '1'
  assertWorkload(parseWorkloadReplicaInput(editingValue) === 1, 'typing one after deleting zero did not produce one replica')

  for (const invalid of ['', '-1', '1.5', 'NaN', 'Infinity', '1e3', '9007199254740992', `1${'0'.repeat(309)}`, 'replicas']) {
    assertWorkload(parseWorkloadReplicaInput(invalid) === undefined, `invalid replica input was accepted: ${invalid || '<blank>'}`)
  }

  assertWorkload(parseWorkloadReplicaInput('0') === 0, 'explicit zero was not valid')
  assertWorkload(parseWorkloadReplicaInput('1') === 1, 'explicit one was not valid')
  assertWorkload(parseWorkloadReplicaInput(String(Number.MAX_SAFE_INTEGER)) === Number.MAX_SAFE_INTEGER, 'safe integer maximum was not valid')
}

runWorkloadModelTests()
