import { useQuery } from '@tanstack/react-query'

import { BlockMeter } from '@/components/block-meter'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import type { Health } from '@/lib/api'
import { formatBytes, percentage } from '@/lib/format'
import { healthQuery } from '@/lib/queries'

// The folder where the installers mount the media of the apps.
const mediaMount = '/mnt/iglo/media'

const fsNames: Record<string, string> = {
    cifs: 'SMB',
    smb3: 'SMB',
    nfs: 'NFS',
    nfs4: 'NFS',
    folder: 'folder on the host',
}

type Space = { key: string; name: string; detail: string; used: number; total: number; problem?: string }

// spaces lists where files live: the media folder first, then the other
// shares, the Proxmox storage and the ZFS pools.
function spaces(health: Health): Space[] {
    const shares = [...health.shares].sort((a, b) => Number(b.path === mediaMount) - Number(a.path === mediaMount))

    return [
        ...shares.map((share) => ({
            key: `share ${share.path}`,
            name: share.path === mediaMount ? 'Media folder' : share.source,
            detail:
                share.path === mediaMount
                    ? `${share.source}, ${fsNames[share.fsType] ?? share.fsType}`
                    : `${share.path}, ${fsNames[share.fsType] ?? share.fsType}`,
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
                    <li key={space.key} className="flex flex-col gap-1.5 px-4 py-3">
                        <div className="flex items-baseline gap-3">
                            <span className="min-w-0 truncate font-medium">{space.name}</span>
                            <span className="ml-auto text-sm whitespace-nowrap tabular-nums">
                                {space.total > 0 ? `${formatBytes(space.used)} / ${formatBytes(space.total)}` : ''}
                            </span>
                        </div>
                        <span className="truncate text-xs text-muted-foreground">{space.detail}</span>
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
