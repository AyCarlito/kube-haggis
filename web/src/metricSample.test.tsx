import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { MetricSample } from './metricSample'

/** Fails a focused metric sample component assertion with a readable message. */
function assertMetricSample(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Exercises accessible metric links, stale-sample link retention, and non-interactive unavailable rows. */
export function runMetricSampleComponentTests(): void {
  const interactive = renderToStaticMarkup(
    <MetricSample href="/resources/core/v1/nodes/worker-a?context=field&namespace=team-a" ariaLabel="Open Node worker-a resource detail">
      <span>worker-a</span>
    </MetricSample>,
  )
  assertMetricSample(interactive.startsWith('<a '), 'metric sample with an exact destination did not render as a link')
  assertMetricSample(interactive.includes('class="metrics-row metrics-row-link"'), 'metric sample link lost its dense row styling')
  assertMetricSample(interactive.includes('aria-label="Open Node worker-a resource detail"'), 'metric sample link did not expose its destination label')
  assertMetricSample(interactive.includes('href="/resources/core/v1/nodes/worker-a?context=field&amp;namespace=team-a"'), 'metric sample link did not retain context and namespace query state')

  const podInteractive = renderToStaticMarkup(
    <MetricSample href="/resources/core/v1/pods/api-0?context=field&namespace=team-a" ariaLabel="Open Pod api-0 in namespace team-a resource detail">
      <span>team-a/api-0</span>
    </MetricSample>,
  )
  assertMetricSample(podInteractive.startsWith('<a ') && podInteractive.includes('aria-label="Open Pod api-0 in namespace team-a resource detail"'), 'namespaced Pod metric sample did not expose its exact target label')
  assertMetricSample(podInteractive.includes('href="/resources/core/v1/pods/api-0?context=field&amp;namespace=team-a"'), 'Pod metric sample link did not retain its namespace query state')

  const unavailable = renderToStaticMarkup(
    <MetricSample ariaLabel="Pod detail unavailable without a namespace">
      <span>api-0</span>
    </MetricSample>,
  )
  assertMetricSample(unavailable.startsWith('<div '), 'metric sample without a destination became an interactive element')
  assertMetricSample(!unavailable.includes('href=') && !unavailable.includes('tabindex=') && !unavailable.includes('role="link"'), 'unavailable metric sample exposed fake link semantics')
}

runMetricSampleComponentTests()
