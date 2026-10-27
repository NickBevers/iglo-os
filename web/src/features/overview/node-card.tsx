import type { ReactNode } from 'react'

import { BlockMeter } from '@/components/block-meter'
import { Card } from '@/components/ui/card'
import type { Guest, ProxmoxNode } from '@/lib/api'
import { formatBytes, percentage } from '@/lib/format'
import { cn } from '@/lib/utils'

// NodeCard is one strip with the numbers of the host, split by hairlines.
export function NodeCard({ node, guests }: { node: ProxmoxNode; guests: Guest[] }) {
    const cpu = Math.round(node.cpu * 100)
    const memory = percentage(node.mem, node.maxMem)
    const disk = percentage(node.disk, node.maxDisk)
    const running = guests.filter((guest) => guest.status === 'running').length
    const containers = guests.filter((guest) => guest.type === 'lxc').length

    return (
        <Card className="gap-0 py-0">
            {/* The 1px gaps over the border color are the hairlines between the figures. */}
            <dl className="grid grid-cols-2 gap-px bg-border lg:grid-cols-4">
                <Figure label="CPU" value={`${cpu}%`} detail={`of ${node.maxCpu} threads`}>
                    <BlockMeter value={cpu} label="CPU of the host" />
                </Figure>
                <Figure
                    label="Memory"
                    value={formatBytes(node.mem)}
                    detail={`of ${formatBytes(node.maxMem)}, ${memory}%`}
                >
                    <BlockMeter value={memory} label="Memory of the host" />
                </Figure>
                <Figure
                    label="Root disk"
                    value={formatBytes(node.disk)}
                    detail={`of ${formatBytes(node.maxDisk)}, ${disk}%`}
                >
                    <BlockMeter value={disk} label="Root disk of the host" />
                </Figure>
                <Figure
                    label="Running"
                    value={`${running} of ${guests.length}`}
                    detail={`${containers} ${containers === 1 ? 'container' : 'containers'}, ${guests.length - containers} ${guests.length - containers === 1 ? 'VM' : 'VMs'}`}
                >
                    {/* One block for each guest: filled when it runs. */}
                    <div className="flex h-2.5 gap-[2px]" aria-hidden="true">
                        {guests.map((guest) => (
                            <span
                                key={guest.id}
                                title={`${guest.name}: ${guest.status}`}
                                className={cn(
                                    'w-5 rounded-[2px]',
                                    guest.status === 'running' ? 'bg-success' : 'bg-foreground/8',
                                )}
                            />
                        ))}
                    </div>
                </Figure>
            </dl>
        </Card>
    )
}

function Figure({
    label,
    value,
    detail,
    children,
}: {
    label: string
    value: string
    detail: string
    children: ReactNode
}) {
    return (
        <div className="flex min-w-0 flex-col gap-2.5 bg-card px-4 py-4">
            <dt className="text-xs text-muted-foreground">{label}</dt>
            <dd className="flex flex-col gap-3">
                <span className="flex flex-wrap items-baseline gap-x-2">
                    <span className="figure">{value}</span>
                    <span className="text-sm text-muted-foreground tabular-nums">{detail}</span>
                </span>
                {children}
            </dd>
        </div>
    )
}
