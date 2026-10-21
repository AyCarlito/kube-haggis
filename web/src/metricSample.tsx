import React, { type ReactNode } from 'react'

/** Describes one landing-page metric sample that may have an exact resource destination. */
export interface MetricSampleProps {
  href?: string
  ariaLabel: string
  children: ReactNode
}

/** Renders a metric sample as a keyboard-accessible link only when its destination is safe. */
export function MetricSample({ href, ariaLabel, children }: MetricSampleProps) {
  if (!href) return <div className="metrics-row">{children}</div>
  return <a className="metrics-row metrics-row-link" href={href} aria-label={ariaLabel}>{children}</a>
}
