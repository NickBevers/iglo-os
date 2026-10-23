import { createFileRoute } from '@tanstack/react-router'

import { WorkloadsPage } from '@/features/workloads/workloads-page'
import { workloadsQuery } from '@/lib/queries'

export const Route = createFileRoute('/_app/workloads')({
    loader: ({ context }) => context.queryClient.prefetchQuery(workloadsQuery),
    component: WorkloadsPage,
})
