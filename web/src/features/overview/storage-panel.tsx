import { useQuery } from '@tanstack/react-query'

import { BlockMeter } from '@/components/block-meter'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import type { Health } from '@/lib/api'
import { formatBytes, percentage } from '@/lib/format'
import { healthQuery } from '@/lib/queries'

// Iglo-OS mounts each share of the Storage page in a folder of its own here.
const sharesRoot = '/mnt/iglo/shares/'

const fsNames: Record<string, string> = {
    cifs: 'SMB',
    smb3: 'SMB',
    nfs: 'NFS',
    nfs4: 'NFS',
}

type Space = { key: string; name: string; detail: string; used: number; total: number; problem?: string }

// spaces lists where files live: the shares of the Storage page first, then
// the other shares, the Proxmox storage and the ZFS pools.
function spaces(health: Health): Space[] {
    const ours = (path: string) => path.startsWith(sharesRoot)
    const shares = [...health.shares].sort((a, b) => Number(ours(b.path)) - Number(ours(a.path)))

    return [
        ...shares.map((share) => ({
            key: `share ${share.path}`,
            name: ours(share.path) ? `Share ${share.path.slice(sharesRoot.length)}` : share.source,
            detail: `${ours(share.path) ? share.source : share.path}, ${fsNames[share.fsType] ?? share.fsType}`,
            used: share.used,
            total: share.size,
            problem: share.problem ?? (!share.mounted ? 'Not mounted' : share.error),
        })),
        ...health.storage.map((storage) => ({
            key: `storage ${storage.node} ${storage.name}`,
            name: storage.name,
            detail: 'Proxmox storage',
            used: storage.used,
            total: storage.total,
            problem: storage.problem,
        })),
        ...health.pools.map((pool) => ({
            key: `pool ${pool.node} ${pool.name}`,
            name: pool.name,
            detail: `ZFS pool, ${pool.health.toLowerCase()}`,
            used: pool.alloc,
            total: pool.size,
            problem: pool.problem,
        })),
    ]
}

export function StoragePanel() {
    const { data } = useQuery(healthQuery)

    if (!data) {
        return <Skeleton className="h-60 rounded-lg" />
    }

    const list = spaces(data)
    if (list.length === 0) {
        return <Card className="p-4 text-sm text-muted-foreground">No storage found yet.</Card>
    }

    return (
        <Card className="gap-0 py-0">
            <ul className="divide-y">
                {list.map((space) => (
                    <li key={space.key} className="flex flex-col gap-1.5 px-4 py-2.5">
                        {/* Name, kind and amount on one line, so the list stays short. */}
                        <div className="flex min-w-0 items-baseline gap-2">
                            <span className="shrink-0 font-medium">{space.name}</span>
                            <span className="min-w-0 truncate text-xs text-muted-foreground">{space.detail}</span>
                            <span className="ml-auto shrink-0 text-sm whitespace-nowrap tabular-nums">
                                {space.total > 0 ? `${formatBytes(space.used)} / ${formatBytes(space.total)}` : ''}
                            </span>
                        </div>
                        {space.total > 0 && (
                            <BlockMeter
                                value={percentage(space.used, space.total)}
                                label={`${space.name} used`}
                                size="sm"
                            />
                        )}
                        {space.problem && <span className="text-xs text-destructive">{space.problem}</span>}
                    </li>
                ))}
            </ul>
        </Card>
    )
}
