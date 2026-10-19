import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { PauseIcon, PlayIcon } from 'lucide-react'

import { BlockMeter } from '@/components/block-meter'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import type { Jellyfin } from '@/lib/api'
import { jellyfinQuery } from '@/lib/queries'

const numbers = new Intl.NumberFormat()

// LibraryPanel is what Jellyfin holds and plays: videos and music.
export function LibraryPanel() {
    const { data } = useQuery(jellyfinQuery)

    if (!data) {
        return <Skeleton className="h-48 rounded-lg" />
    }

    if (!data.configured || data.error) {
        return (
            <Card className="p-4 text-sm text-muted-foreground">
                <p>
                    {data.error ? `Jellyfin does not answer: ${data.error}. ` : 'Jellyfin is not connected yet. '}
                    <Link to="/jellyfin" className="text-brand hover:underline">
                        Open the Jellyfin page
                    </Link>
                </p>
            </Card>
        )
    }

    return (
        // The panel picks its columns from its own width, not the screen's.
        <Card className="@container gap-0 py-0">
            <div className="grid gap-px bg-border @xl:grid-cols-2 @5xl:grid-cols-[minmax(0,5fr)_minmax(0,4fr)_minmax(0,3fr)]">
                <Counts jellyfin={data} />
                <NowPlaying jellyfin={data} />
                <Recent jellyfin={data} />
            </div>
        </Card>
    )
}

function Counts({ jellyfin }: { jellyfin: Jellyfin }) {
    const groups = [
        {
            title: 'Videos',
            items: [
                ['Movies', jellyfin.movies],
                ['Series', jellyfin.series],
                ['Episodes', jellyfin.episodes],
            ],
        },
        {
            title: 'Music',
            items: [
                ['Albums', jellyfin.albums],
                ['Songs', jellyfin.songs],
                ['Music videos', jellyfin.musicVideos],
            ],
        },
    ] as const

    return (
        <div className="flex flex-col gap-4 bg-card px-4 py-4">
            {groups.map((group) => (
                <div key={group.title} className="flex flex-col gap-1.5">
                    <h3 className="text-xs text-muted-foreground">{group.title}</h3>
                    <dl className="grid grid-cols-3 gap-3">
                        {group.items.map(([label, value]) => (
                            <div key={label} className="flex min-w-0 flex-col">
                                {/* The term comes first, the number shows on top. */}
                                <dt className="order-2 truncate text-xs text-muted-foreground">{label}</dt>
                                <dd className="order-1 text-xl font-semibold wide tabular-nums">
                                    {numbers.format(value)}
                                </dd>
                            </div>
                        ))}
                    </dl>
                </div>
            ))}
        </div>
    )
}

function NowPlaying({ jellyfin }: { jellyfin: Jellyfin }) {
    return (
        <div className="flex min-w-0 flex-col gap-2.5 bg-card px-4 py-4">
            <h3 className="text-xs text-muted-foreground">Playing now</h3>
            {jellyfin.nowPlaying.length === 0 ? (
                <p className="text-sm text-muted-foreground">Nothing plays right now.</p>
            ) : (
                <ul className="flex flex-col gap-3">
                    {jellyfin.nowPlaying.map((session) => (
                        <li key={`${session.user} ${session.device} ${session.itemId}`} className="flex flex-col gap-1">
                            <div className="flex min-w-0 items-center gap-2">
                                {session.paused ? (
                                    <PauseIcon
                                        className="size-3.5 shrink-0 text-muted-foreground"
                                        aria-label="Paused"
                                    />
                                ) : (
                                    <PlayIcon className="size-3.5 shrink-0 text-brand" aria-label="Playing" />
                                )}
                                <span className="truncate text-sm font-medium">{session.title}</span>
                            </div>
                            <span className="truncate pl-5.5 text-xs text-muted-foreground">
                                {session.subtitle ? `${session.subtitle}. ` : ''}
                                {session.user} on {session.device}
                                {session.transcode ? ', transcoding' : ''}
                            </span>
                            <BlockMeter
                                kind="progress"
                                value={session.progress}
                                label={`Progress of ${session.title}`}
                                size="sm"
                                className="pl-5.5"
                            />
                        </li>
                    ))}
                </ul>
            )}
        </div>
    )
}

function Recent({ jellyfin }: { jellyfin: Jellyfin }) {
    return (
        <div className="flex min-w-0 flex-col gap-2.5 bg-card px-4 py-4 @xl:col-span-2 @5xl:col-span-1">
            <h3 className="text-xs text-muted-foreground">Added lately</h3>
            {jellyfin.recent.length === 0 ? (
                <p className="text-sm text-muted-foreground">Nothing new yet.</p>
            ) : (
                <ul className="grid gap-x-6 gap-y-1.5 @xl:grid-cols-2 @5xl:grid-cols-1">
                    {jellyfin.recent.slice(0, 6).map((item) => (
                        <li key={item.id} className="flex min-w-0 items-baseline gap-2 text-sm">
                            <span className="truncate">{item.name}</span>
                            <span className="ml-auto shrink-0 text-xs text-muted-foreground tabular-nums">
                                {item.type === 'Series' ? 'series' : 'movie'}
                                {item.year ? `, ${item.year}` : ''}
                            </span>
                        </li>
                    ))}
                </ul>
            )}
        </div>
    )
}
