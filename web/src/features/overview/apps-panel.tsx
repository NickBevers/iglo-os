import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ExternalLinkIcon } from 'lucide-react'

import { BlockMeter } from '@/components/block-meter'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { api, type AppStats, type CatalogApp, type Jellyfin } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { appsQuery, jellyfinQuery } from '@/lib/queries'
import { cn } from '@/lib/utils'

const numbers = new Intl.NumberFormat()

const actionNames: Record<string, string> = {
    install: 'Installing',
    update: 'Updating',
    remove: 'Removing',
    vpn: 'Changing the VPN',
    players: 'Changing the players',
}

// AppsPanel shows each app of the catalog: whether it runs, and what it does right now.
export function AppsPanel() {
    const { data } = useQuery(appsQuery)
    const { data: jellyfin } = useQuery(jellyfinQuery)

    if (!data) {
        return <Skeleton className="h-60 rounded-lg" />
    }

    // A new server points to the guided setup, which installs everything in one go.
    const fresh = !data.apps.some((app) => app.installed || app.operation?.state === 'running')

    return (
        // Two columns on wide screens. The 1px gaps over the border color are the hairlines.
        <Card className="gap-0 py-0">
            {fresh && (
                <p className="border-b px-4 py-3 text-sm">
                    Nothing is installed yet.{' '}
                    <Link to="/setup" className="font-medium underline">
                        Set up everything in one go
                    </Link>
                    , or install single apps from Apps.
                </p>
            )}
            <ul className="grid gap-px bg-border lg:grid-cols-2">
                {data.apps.map((app) => (
                    <AppRow key={app.id} app={app} jellyfin={jellyfin} />
                ))}
            </ul>
        </Card>
    )
}

function AppRow({ app, jellyfin }: { app: CatalogApp; jellyfin?: Jellyfin }) {
    const busy = app.operation?.state === 'running'
    const running = app.installed && app.status === 'running'
    // Gaming only runs while you play, so stopped is its normal state.
    const resting = app.id === 'gaming' && app.installed && app.status === 'stopped'

    const state = busy
        ? `${actionNames[app.operation?.action ?? 'install'] ?? 'Working'}…`
        : !app.installed
          ? app.planned
              ? 'Integration planned'
              : 'Not installed'
          : app.status === 'running'
            ? 'Running'
            : resting
              ? 'Starts when you play'
              : (app.status ?? 'Stopped')

    return (
        <li className="flex flex-col gap-2 bg-card px-4 py-3">
            <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
                <span
                    className={cn(
                        'size-2 shrink-0 rounded-full',
                        busy
                            ? 'animate-pulse bg-brand'
                            : running
                              ? 'bg-success'
                              : app.installed && !resting
                                ? 'bg-warning'
                                : 'bg-muted-foreground/40',
                    )}
                    aria-hidden="true"
                />
                <span className="font-medium">{app.name}</span>
                <span className="text-sm text-muted-foreground">{state}</span>
                {app.installed && app.planned && <Badge variant="outline">Integration planned</Badge>}
                {app.stats?.sample && <Badge variant="outline">Sample data</Badge>}
                {app.updateAvailable && !busy && <Badge variant="warning">Update available</Badge>}
                <div className="ml-auto flex items-center gap-1">
                    {app.hostUrl && running && (
                        <a
                            href={app.hostUrl}
                            target="_blank"
                            rel="noreferrer"
                            className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-sm text-brand hover:bg-accent"
                        >
                            Open
                            <ExternalLinkIcon className="size-3.5" aria-hidden="true" />
                        </a>
                    )}
                    {(app.installed || !app.planned) && (
                        <Link
                            to={
                                app.installed && app.id === 'jellyfin'
                                    ? '/jellyfin'
                                    : app.installed && app.id === 'gaming'
                                      ? '/gaming'
                                      : '/apps'
                            }
                            className="rounded-md px-2 py-1 text-sm text-muted-foreground hover:bg-accent hover:text-foreground"
                        >
                            {app.planned ? 'Details' : app.installed ? 'Manage' : 'Install'}
                            <span className="sr-only"> {app.name}</span>
                        </Link>
                    )}
                </div>
            </div>
            {app.installed && <AppFacts app={app} running={running} jellyfin={jellyfin} />}
        </li>
    )
}

function AppFacts({ app, running, jellyfin }: { app: CatalogApp; running: boolean; jellyfin?: Jellyfin }) {
    if (app.stats) {
        return <StatsFacts stats={app.stats} names={app.id === 'gaming'} />
    }
    if (app.planned) {
        return <Facts>{running ? 'It reports nothing yet.' : 'It is not running.'}</Facts>
    }
    if (app.id === 'jellyfin') {
        if (!jellyfin?.configured) {
            return <Facts>Connect it on the Jellyfin page to see the library.</Facts>
        }
        if (jellyfin.error) {
            return <Facts>Jellyfin does not answer: {jellyfin.error}</Facts>
        }
        const playing = jellyfin.nowPlaying.length
        return (
            <Facts>
                {numbers.format(jellyfin.movies)} movies, {numbers.format(jellyfin.series)} series and{' '}
                {numbers.format(jellyfin.songs)} songs.{' '}
                {playing === 0 ? 'Nothing plays right now.' : `${playing} playing now.`}
            </Facts>
        )
    }

    if (app.id === 'minecraft') {
        return <MinecraftFacts app={app} running={running} />
    }

    // The media stack: one link for each app in it.
    const links = app.links.filter((link) => link.url)
    if (links.length === 0) {
        return <Facts>{app.links.map((link) => link.name).join(', ')}</Facts>
    }
    return (
        <div className="flex flex-wrap gap-1.5 pl-[18px]">
            {links.map((link) => (
                <a
                    key={link.name}
                    href={link.url}
                    target="_blank"
                    rel="noreferrer"
                    className="rounded-md border px-2 py-0.5 text-xs hover:bg-accent"
                    title={link.description}
                >
                    {link.name}
                </a>
            ))}
        </div>
    )
}

function MinecraftFacts({ app, running }: { app: CatalogApp; running: boolean }) {
    const { data: players } = useQuery({
        queryKey: ['apps', 'minecraft', 'players'],
        queryFn: api.minecraftPlayers,
        enabled: running,
    })
    const address = app.links.find((link) => link.address)?.url

    return (
        <Facts>
            {address && (
                <>
                    At home: <span className="font-mono text-xs text-foreground select-all">{address}</span>.{' '}
                </>
            )}
            {app.publicAddress && (
                <>
                    Friends: <span className="font-mono text-xs text-foreground select-all">{app.publicAddress}</span>
                    .{' '}
                </>
            )}
            {players &&
                `${players.whitelist.length} ${players.whitelist.length === 1 ? 'player' : 'players'} on the whitelist.`}
        </Facts>
    )
}

// StatsFacts are the figures of an app in one line, and what it does now.
// With names, the labels are names like Game Boy and keep their capitals.
function StatsFacts({ stats, names = false }: { stats: AppStats; names?: boolean }) {
    return (
        <div className="flex flex-col gap-2.5 pl-[18px]">
            <p className="text-sm text-muted-foreground">
                {stats.figures
                    .map((figure) =>
                        figure.unit === 'bytes'
                            ? `${formatBytes(figure.value)} ${figure.label.toLowerCase()}`
                            : `${numbers.format(figure.value)} ${names ? figure.label : figure.label.toLowerCase()}`,
                    )
                    .join(', ')}
            </p>
            {stats.activity.length > 0 && (
                <ul className="flex flex-col gap-2">
                    {stats.activity.map((activity) => (
                        <li key={activity.title} className="flex flex-col gap-1">
                            <div className="flex min-w-0 items-baseline gap-2 text-sm">
                                <span className="truncate font-medium">{activity.title}</span>
                                <span className="truncate text-xs text-muted-foreground">{activity.detail}</span>
                                {activity.progress >= 0 && (
                                    <span className="ml-auto shrink-0 text-xs tabular-nums">{activity.progress}%</span>
                                )}
                            </div>
                            {activity.progress >= 0 && (
                                <BlockMeter
                                    kind="progress"
                                    value={activity.progress}
                                    label={`Progress of ${activity.title}`}
                                    size="sm"
                                />
                            )}
                        </li>
                    ))}
                </ul>
            )}
        </div>
    )
}

function Facts({ children }: { children: ReactNode }) {
    return <p className="pl-[18px] text-sm text-muted-foreground">{children}</p>
}
