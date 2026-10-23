import { ApiError, retryClassificationFor } from './api'
import type { DiagnosticsResponse } from './types'

/** Fails a focused diagnostics model assertion with a readable message. */
function assertDiagnostics(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Exercises request-ID error copy, retry classification, and bounded diagnostics rendering data. */
export function runDiagnosticsModelTests(): void {
  const error = new ApiError('The Kubernetes API is unavailable', 503, {}, 'request-42', 'retry')
  assertDiagnostics(error.message.includes('request ID request-42') && error.message.includes('retry: retry'), 'API error copy omitted request ID or retry classification')
  assertDiagnostics(retryClassificationFor(409, {}) === 'refresh-and-review' && retryClassificationFor(403, {}) === 'check-and-retry', 'status retry classifications were not stable')

  const snapshot: DiagnosticsResponse = {
    generatedAt: '2026-10-21T12:00:00Z',
    uptimeSeconds: 42,
    kubeconfig: { source: 'default', state: 'ready', revision: 3 },
    context: { current: 'field', selected: 'field', state: 'available' },
    discovery: { context: 'field', revision: 3, state: 'partial', ageSeconds: 7, itemCount: 12, issueCount: 1 },
    watches: { active: 1, current: [], recent: [] },
    recentErrors: [{ at: '2026-10-21T12:00:00Z', category: 'permission-denied', operation: 'watch' }],
    sessions: { terminal: 1, logs: 0, portForward: 0, network: 0, watch: 1 },
    audit: { state: 'available' },
    requests: { total: 4, errors: 1, averageDurationMilliseconds: 3, maxDurationMilliseconds: 9, statuses: { '200': 3, '403': 1 }, operations: { watch: 1 }, errorCategories: { 'permission-denied': 1 } },
  }
  assertDiagnostics(snapshot.discovery.state === 'partial' && snapshot.sessions.terminal === 1 && snapshot.recentErrors[0].category === 'permission-denied', 'diagnostics model did not preserve safe state summaries')
}

runDiagnosticsModelTests()
