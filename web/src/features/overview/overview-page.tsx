import { useQuery } from '@tanstack/react-query'
import { AlertTriangleIcon } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { formatUptime } from '@/lib/format'
import { overviewQuery } from '@/lib/queries'
import { AppsPanel } from './apps-panel'
import { GuestList } from './guest-list'
import { LibraryPanel } from './library-panel'
import { NodeCard } from './node-card'
import { StoragePanel } from './storage-panel'
import { Summaries } from './summaries'
import { UsageCharts } from './usage-charts'

export function OverviewPage() {
    const { data, error, isPending } = useQuery(overviewQuery)

    if (isPending) {
        return (
            <div className="flex flex-col gap-6">
                <Skeleton className="h-8 w-48" />
                <Skeleton className="h-32 w-full rounded-lg" />
                <Skeleton className="h-28 w-full rounded-lg" />
                <Skeleton className="h-64 w-full rounded-lg" />
            </div>
        )
    }

    if (error) {
        return (
            <Alert variant="destructive">
                <AlertTriangleIcon />
                <AlertTitle>Could not load your server</AlertTitle>
                <AlertDescription>{error.message}</AlertDescription>
            </Alert>
        )
    }

    const node = data.nodes[0]
    // pveVersion looks like pve-manager/9.2.0/abcdef.
    const version = node?.pveVersion.split('/')[1]

    return (
        <div className="flex flex-col gap-6">
            <header className="flex flex-col gap-1">
                <h1 className="page-title">Overview</h1>
                {node && (
                    <p className="text-sm text-muted-foreground">
                        {node.name} runs Proxmox VE {version ?? ''}, and has been up for {formatUptime(node.uptime)}.
                    </p>
                )}
            </header>

            {data.nodes.map((node) => (
                <NodeCard key={node.name} node={node} guests={data.guests} />
            ))}

            <Summaries />

            {/* Apps and the library on the left, the storage next to them. */}
            <div className="grid gap-6 lg:grid-cols-5">
                <div className="flex flex-col gap-6 lg:col-span-3">
                    <section className="flex flex-col gap-2.5" aria-labelledby="apps-title">
                        <h2 id="apps-title" className="section-title">
                            Apps
                        </h2>
                        <AppsPanel />
                    </section>
                    <section className="flex flex-col gap-2.5" aria-labelledby="library-title">
                        <h2 id="library-title" className="section-title">
                            Library
                        </h2>
                        <LibraryPanel />
                    </section>
                </div>
                <section className="flex flex-col gap-2.5 lg:col-span-2" aria-labelledby="storage-title">
                    <h2 id="storage-title" className="section-title">
                        Storage
                    </h2>
                    <StoragePanel />
                </section>
            </div>

            <section className="flex flex-col gap-2.5" aria-labelledby="guests-title">
                <h2 id="guests-title" className="section-title">
                    Containers and VMs
                </h2>
                <GuestList guests={data.guests} />
            </section>

            {node && (
                <section className="flex flex-col gap-2.5" aria-label="History of the host">
                    <Card className="px-4">
                        <UsageCharts target={{ node: node.name }} />
                    </Card>
                </section>
            )}
        </div>
    )
}
