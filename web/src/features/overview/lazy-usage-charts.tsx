import { lazy, Suspense } from 'react'

import type { UsageTarget } from '@/lib/api'

// The charts library is large, so it loads with the first chart, not with every page.
const UsageCharts = lazy(() => import('./usage-charts').then((module) => ({ default: module.UsageCharts })))

export function LazyUsageCharts({ target }: { target: UsageTarget }) {
    return (
        <Suspense fallback={<p className="py-6 text-sm text-muted-foreground">Loading the history…</p>}>
            <UsageCharts target={target} />
        </Suspense>
    )
}
