import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CircleCheckIcon, ExternalLinkIcon, Loader2Icon, RotateCwIcon, TriangleAlertIcon } from 'lucide-react'
import { toast } from 'sonner'

import { GuestId } from '@/components/guest-id'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { api, type Apps, type CatalogApp } from '@/lib/api'
import { appsQuery } from '@/lib/queries'
import { formatDateTime, formatRelative } from '@/lib/format'
import { AppActions } from './app-actions'
import { HeadscaleDetails } from './headscale-details'
import { InstallDialog } from './install-dialog'

// On a phone, the buttons go below the description, so the text keeps its width.
const actionClass =
    'flex flex-wrap justify-end gap-2 max-sm:col-start-1 max-sm:row-span-1 max-sm:row-start-auto max-sm:justify-self-start max-sm:justify-start max-sm:pt-2'

export function AppsPage() {
    const { data, error } = useQuery(appsQuery)

    return (
        <div className="flex flex-col gap-6">
            <header>
                <h1 className="page-title">Apps</h1>
                <p className="text-sm text-muted-foreground">
                    Each app gets its own container on this host, with its own backups and updates.
                </p>
            </header>

            {error && (
                <Alert variant="destructive">
                    <TriangleAlertIcon />
                    <AlertTitle>Could not load the apps</AlertTitle>
                    <AlertDescription>{error.message}</AlertDescription>
                </Alert>
            )}
            {data?.error && (
                <Alert variant="destructive">
                    <TriangleAlertIcon />
                    <AlertTitle>The host agent did not answer</AlertTitle>
                    <AlertDescription>{data.error}</AlertDescription>
                </Alert>
            )}

            {!data && !error && <Skeleton className="h-48 w-full rounded-xl" />}
            {data?.apps.map((app) => (
                <AppCard key={app.id} app={app} defaults={data.defaults} />
            ))}
        </div>
    )
}

function AppCard({ app, defaults }: { app: CatalogApp; defaults: Apps['defaults'] }) {
    const queryClient = useQueryClient()
    const [installOpen, setInstallOpen] = useState(false)
    const operation = app.operation
    const running = operation?.state === 'running'
    const saved = app.saved

    const refresh = () => queryClient.invalidateQueries({ queryKey: appsQuery.queryKey })
    const retry = useMutation({
        mutationFn: () => api.retryApp(app.id),
        onSuccess: refresh,
        onError: (error) => toast.error(error.message),
    })
    const forget = useMutation({
        mutationFn: () => api.forgetAppAnswers(app.id),
        onSuccess: () => {
            refresh()
            toast.success('The answers are removed from the host')
        },
        onError: (error) => toast.error(error.message),
    })

    return (
        <Card>
            <CardHeader className="max-sm:has-data-[slot=card-action]:grid-cols-1">
                <CardTitle className="flex flex-wrap items-center gap-2">
                    {app.name}
                    {app.planned && <Badge variant="outline">Integration planned</Badge>}
                    {app.stats?.sample && <Badge variant="outline">Sample data</Badge>}
                    {app.installed && app.vmid !== undefined && <GuestId vmid={app.vmid} />}
                    {app.installed && (
                        <Badge variant={app.status === 'running' ? 'success' : 'outline'}>{app.status}</Badge>
                    )}
                    {app.updateAvailable && !running && <Badge variant="warning">Update available</Badge>}
                </CardTitle>
                <CardDescription className="flex flex-col gap-1">
                    <span>{app.description}</span>
                    {app.hostUrl && (
                        <a
                            href={app.hostUrl}
                            target="_blank"
                            rel="noreferrer"
                            className="w-fit font-medium text-brand hover:underline"
                        >
                            {app.hostUrl.replace('https://', '')}
                        </a>
                    )}
                </CardDescription>
                {app.installed && app.managed && !app.planned && !running && (
                    <CardAction className={actionClass}>
                        <AppActions app={app} />
                    </CardAction>
                )}
                {!app.installed && !app.planned && !running && (
                    <CardAction className={actionClass}>
                        {saved ? (
                            <>
                                <Button variant="outline" onClick={() => setInstallOpen(true)}>
                                    Change answers
                                </Button>
                                {/* The media stack keeps no VPN login for a retry. */}
                                {!['media', 'headscale'].includes(app.id) && (
                                    <Button disabled={retry.isPending} onClick={() => retry.mutate()}>
                                        {retry.isPending ? <Loader2Icon className="animate-spin" /> : <RotateCwIcon />}
                                        Try again
                                    </Button>
                                )}
                            </>
                        ) : (
                            <Button onClick={() => setInstallOpen(true)}>Install</Button>
                        )}
                    </CardAction>
                )}
            </CardHeader>

            <CardContent className="flex flex-col gap-4">
                {operation && (
                    <OperationProgress operation={operation} rollback={app.rollback} flareSolverr={app.flareSolverr} />
                )}
                {app.id === 'headscale' && app.installed && <HeadscaleDetails app={app} />}
                {app.planned && (
                    <p className="text-xs text-muted-foreground">
                        This app's Iglo-OS integration is not implemented yet. Installation, updates and removal are
                        unavailable here.
                    </p>
                )}
                {app.stats?.sample && (
                    <p className="text-xs text-muted-foreground">
                        This test preview uses a sample container and statistics. It does not represent an installed app
                        on your server.
                    </p>
                )}
                {app.installed && !app.managed && (
                    <p className="text-xs text-muted-foreground">
                        Iglo-OS did not install this container, so it can't update or remove it here. Update it on the
                        Updates page.
                    </p>
                )}
                {saved && !running && (
                    <p className="text-xs text-muted-foreground">
                        Your answers stay on the host until {formatDateTime(saved.until)}, so you can try again without
                        typing them. The keys and passwords never leave the host.{' '}
                        <button
                            type="button"
                            className="underline"
                            disabled={forget.isPending}
                            onClick={() => forget.mutate()}
                        >
                            Remove them now
                        </button>
                    </p>
                )}
                <ul className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                    {app.links.map((link) => (
                        <li key={link.name}>
                            {link.url ? (
                                <a
                                    href={link.url}
                                    target="_blank"
                                    rel="noreferrer"
                                    className="flex items-center justify-between gap-2 rounded-lg border px-3 py-2 text-sm hover:bg-muted/50"
                                >
                                    <span className="min-w-0">
                                        <span className="block font-medium">{link.name}</span>
                                        <span className="block text-xs text-muted-foreground">{link.description}</span>
                                    </span>
                                    <ExternalLinkIcon className="size-4 text-muted-foreground" />
                                </a>
                            ) : (
                                <div className="rounded-lg border border-dashed px-3 py-2 text-sm text-muted-foreground">
                                    <span className="block font-medium text-foreground">{link.name}</span>
                                    <span className="block text-xs">{link.description}</span>
                                </div>
                            )}
                        </li>
                    ))}
                </ul>
                {app.installed && app.id === 'media' && (
                    <p className="text-xs text-muted-foreground">
                        Log in to Seerr with your Jellyfin account. If you gave no Jellyfin admin during the install,
                        finish the setup of Seerr first: until then, anyone on your network can open it. Log in to the
                        other apps with the username and password from the install. Add your indexers in Prowlarr. They
                        sync to Radarr and Sonarr. For anime in Japanese with English subtitles, set the audio and
                        subtitle languages of your user in Jellyfin.
                    </p>
                )}
                {app.id === 'media' && app.flareSolverr !== undefined && (
                    <p className="text-xs text-muted-foreground">
                        {app.flareSolverr
                            ? 'FlareSolverr runs. Give indexers behind Cloudflare the flaresolverr tag in Prowlarr.'
                            : 'FlareSolverr is off. Turn it on under FlareSolverr when an indexer is behind Cloudflare.'}
                    </p>
                )}
            </CardContent>

            <InstallDialog
                app={app}
                defaults={defaults}
                saved={saved}
                open={installOpen}
                onOpenChange={setInstallOpen}
            />
        </Card>
    )
}

const operationText = {
    backup: {
        running: 'Backing up Photos…',
        runningDetail: 'Photos is paused while originals and the database are copied. Large libraries can take hours.',
        failed: 'The Photos backup failed',
        succeeded: 'Photos is backed up',
    },
    verify: {
        running: 'Checking Photos recovery…',
        runningDetail: 'Checksums are checked and a disposable database is restored. Live data stays in place.',
        failed: 'Recovery verification failed',
        succeeded: 'Recovery verified',
    },
    install: {
        running: 'Installing…',
        runningDetail: 'This takes 5 to 15 minutes, mostly to download the images. You can leave this page.',
        failed: 'The install failed',
        succeeded: 'Installed',
    },
    update: {
        running: 'Updating…',
        runningDetail: 'This takes a few minutes. The app is offline while it restarts. You can leave this page.',
        failed: 'The update failed',
        succeeded: 'Updated',
    },
    remove: {
        running: 'Removing…',
        runningDetail: 'You can leave this page.',
        failed: 'The removal failed',
        succeeded: 'Removed',
    },
    vpn: {
        running: 'Changing the VPN…',
        runningDetail: 'Downloads stop for a minute while the VPN restarts. You can leave this page.',
        failed: 'The new VPN settings did not connect',
        succeeded: 'The new VPN settings connect',
    },
    flaresolverr: {
        running: 'Changing FlareSolverr…',
        runningDetail:
            'The other apps keep running. Prowlarr gets or loses its FlareSolverr proxy. You can leave this page.',
        failed: 'The FlareSolverr change failed',
        succeeded: 'FlareSolverr is changed',
    },
}

function OperationProgress({
    operation,
    rollback,
    flareSolverr,
}: {
    operation: NonNullable<CatalogApp['operation']>
    rollback?: string
    // flareSolverr is the switch of the media stack, for a FlareSolverr change that worked.
    flareSolverr?: boolean
}) {
    const log = useRef<HTMLPreElement>(null)
    // An agent from before updates and removals sends no action.
    const text = operationText[operation.action ?? 'install']

    // Follow the log while it grows.
    useEffect(() => {
        log.current?.scrollTo({ top: log.current.scrollHeight })
    }, [operation.log])

    const logView = operation.log && (
        <pre
            ref={log}
            className="max-h-72 overflow-auto rounded-md bg-muted p-3 font-mono text-xs whitespace-pre-wrap text-foreground"
        >
            {operation.log}
        </pre>
    )

    if (operation.state === 'running') {
        return (
            <div className="flex flex-col gap-3">
                <Alert>
                    <Loader2Icon className="animate-spin" />
                    <AlertTitle>{text.running}</AlertTitle>
                    <AlertDescription>{text.runningDetail}</AlertDescription>
                </Alert>
                {logView}
            </div>
        )
    }

    if (operation.state === 'failed') {
        return (
            <Alert variant="destructive">
                <TriangleAlertIcon />
                <AlertTitle>{text.failed}</AlertTitle>
                <AlertDescription className="flex flex-col gap-2">
                    <span>
                        {operation.message}.
                        {operation.action === 'install' && ' If the install made a container, it removed it again.'}
                        {operation.action === 'vpn' && ' The old settings are back.'}
                        {operation.action === 'flaresolverr' && ' FlareSolverr stays as it was.'}
                        {rollback && ` ${rollback}`}
                    </span>
                    {logView && (
                        <details open>
                            <summary className="cursor-pointer">Log</summary>
                            <div className="mt-2">{logView}</div>
                        </details>
                    )}
                </AlertDescription>
            </Alert>
        )
    }

    // An operation that worked shows for an hour, so a long install doesn't
    // just disappear. role="status" announces it without interrupting.
    if (operation.state === 'succeeded') {
        return (
            <Alert role="status">
                <CircleCheckIcon className="text-success" />
                <AlertTitle>
                    {operation.action === 'flaresolverr' && flareSolverr !== undefined
                        ? `FlareSolverr is ${flareSolverr ? 'on' : 'off'}`
                        : text.succeeded}
                    {operation.finishedAt && ` ${formatRelative(operation.finishedAt)}`}
                </AlertTitle>
                {operation.message && <AlertDescription>{operation.message}</AlertDescription>}
            </Alert>
        )
    }

    return null
}
