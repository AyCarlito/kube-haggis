import React from 'react'
import type { ComputeQuantitySet, ComputeQuantityValue, ComputeReadPackData, ComputeUsageSummary } from './types'

/** Renders the normalized Compute panel for one core/v1 Node or Pod. */
export function ComputeReadPackView({ data }: { data: ComputeReadPackData }) {
  const nodeSemantics = data.kind === 'Node'
  const semantics = nodeSemantics
    ? 'Kubernetes does not expose Node-level requests or limits equivalent to Pod resource requirements. Capacity and allocatable are native Node fields.'
    : 'Pod requests and limits are summed from container specifications using Kubernetes quantity semantics. Capacity and allocatable do not apply to Pods.'
  return (
    <div className="read-pack-body compute-read-pack-body">
      <div className="compute-identity-grid">
        <div><span>Identity</span><strong>{data.kind}/{data.name}</strong></div>
        <div><span>Namespace</span><strong>{data.namespace || 'cluster scope'}</strong></div>
        <div><span>Actual usage</span><strong className={`compute-telemetry-${data.usage.state}`}>{computeTelemetryStateLabel(data.usage.state)}</strong></div>
        <div><span>Sample timestamp</span><strong>{data.usage.timestamp || 'not reported'}</strong></div>
      </div>
      <div className="compute-semantics"><span>{semantics}</span>{data.usage.message && <small>{data.usage.message}</small>}</div>
      <div className="read-pack-section">
        <div className="read-pack-section-heading"><span>CPU and memory</span></div>
        <div className="read-pack-table compute-table">
          <div className="compute-table-row compute-table-header"><strong>Resource</strong><strong>Requests</strong><strong>Limits</strong><strong>Capacity</strong><strong>Allocatable</strong><strong>Actual usage</strong></div>
          <ComputeTableRow label="CPU" requests={data.requests} limits={data.limits} capacity={data.capacity} allocatable={data.allocatable} usage={data.usage} resource="cpu" />
          <ComputeTableRow label="Memory" requests={data.requests} limits={data.limits} capacity={data.capacity} allocatable={data.allocatable} usage={data.usage} resource="memory" />
        </div>
      </div>
    </div>
  )
}

/** Renders one CPU or memory row with consistently labeled quantity states. */
function ComputeTableRow({ label, requests, limits, capacity, allocatable, usage, resource }: { label: string; requests: ComputeQuantitySet; limits: ComputeQuantitySet; capacity: ComputeQuantitySet; allocatable: ComputeQuantitySet; usage: ComputeUsageSummary; resource: 'cpu' | 'memory' }) {
  const request = requests[resource]
  const limit = limits[resource]
  const capacityValue = capacity[resource]
  const allocatableValue = allocatable[resource]
  const actual = usage[resource]
  return <div className="compute-table-row"><strong>{label}</strong><ComputeValueCell value={request} /><ComputeValueCell value={limit} /><ComputeValueCell value={capacityValue} /><ComputeValueCell value={allocatableValue} /><ComputeValueCell value={actual} usageState={usage.state} /></div>
}

/** Renders one bounded quantity value without exposing arbitrary object fields. */
function ComputeValueCell({ value, usageState }: { value: ComputeQuantityValue; usageState?: ComputeUsageSummary['state'] }) {
  const displayState = usageState && value.state === 'available' && usageState !== 'available' ? usageState : value.state
  const text = value.value || computeValueStateLabel(displayState)
  const stateLabel = displayState === 'available' ? '' : computeValueStateLabel(displayState)
  return <span className={`compute-value-cell compute-value-${displayState}`}><strong>{text}</strong>{stateLabel && <small>{stateLabel}</small>}</span>
}

/** Converts an object-side or telemetry value state into explicit operator-facing copy. */
export function computeValueStateLabel(state: ComputeQuantityValue['state']): string {
  if (state === 'available') return 'set'
  if (state === 'absent') return 'not set'
  if (state === 'partial') return 'partial'
  if (state === 'not-applicable') return 'not applicable'
  return computeTelemetryStateLabel(state)
}

/** Converts a native metrics state into explicit Compute telemetry copy. */
export function computeTelemetryStateLabel(state: ComputeUsageSummary['state']): string {
  if (state === 'available') return 'Available'
  if (state === 'stale') return 'Stale'
  if (state === 'permission-denied') return 'RBAC denied'
  if (state === 'unavailable') return 'Unavailable'
  if (state === 'empty') return 'Empty'
  if (state === 'partial') return 'Partial'
  if (state === 'error') return 'Read error'
  return 'Unknown'
}
