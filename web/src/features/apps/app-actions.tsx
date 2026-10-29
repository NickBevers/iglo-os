import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowUpCircleIcon, CloudIcon, Loader2Icon, ShieldIcon, Trash2Icon } from 'lucide-react'
import { toast } from 'sonner'

import {
    AlertDialog,
    AlertDialogAction,
    AlertDialogCancel,
    AlertDialogContent,
    AlertDialogDescription,
    AlertDialogFooter,
    AlertDialogHeader,
    AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { api, type CatalogApp } from '@/lib/api'
import { appsQuery } from '@/lib/queries'
import { FlareSolverrDialog } from './flaresolverr-dialog'
import { VpnDialog } from './vpn-dialog'

const updateText: Record<string, string> = {
    headscale:
        'The controller and relay pause briefly. Iglo keeps a checked local database backup and preserves device identities, certificates and policy. Unsupported version changes are refused. An interrupted upgrade can lose registrations made after its backup.',
    music: 'Iglo-OS updates the worker and its pinned download tools. An active job can be interrupted and will need resubmission; queued jobs and published NAS files stay.',
    inbox: 'Iglo-OS updates the worker and FFmpeg. An active job can be interrupted and will need a retry; queued jobs and published NAS files stay.',
    photos: 'Iglo-OS backs up the database and the originals with restic first, like the nightly backup, then installs the Immich version bundled with this Iglo-OS release. Photos restarts, so it is offline for a few minutes.',
    media: 'Iglo-OS makes a snapshot, updates the packages, copies the stack of this Iglo-OS release, and downloads the new images. The apps restart, so they are offline for a few minutes. Your settings in the apps stay. Recyclarr then names new files the way Jellyfin reads them, and adds the anime profile to Radarr; files that are there keep their names.',
    jellyfin:
        'Iglo-OS makes a snapshot, then updates Jellyfin and the other packages. Jellyfin restarts, so it is offline for a minute. Your settings, users and libraries stay.',
}

const removeText: Record<string, string> = {
    headscale:
        'The controller database, device identities and TLS certificates are deleted. Every device must join again unless you restore a backup. If Iglo uses this controller, its remote access will stop. Keep LAN access available. Repeated reinstalls may hit certificate issuance limits.',
    music: 'The queue and any unfinished staging files are deleted. Published music on the NAS stays.',
    inbox: 'Worker settings and history on the local disk are deleted. NAS sources and completed outputs stay.',
    photos: 'The local database is deleted with the container. Back up Photos and run Verify recovery first. Originals, the backups on the NAS and off it, and their password on the host stay.',
    media: 'The downloads disk goes with it, so downloads that did not finish are lost. Your movies and series stay on the Videos share, and Jellyfin keeps its libraries.',
    jellyfin:
        'Its settings, users and watch history go with it, and the Jellyfin page disconnects. Your movies and series stay on the Videos share.',
}

// AppActions are the buttons of an app that Iglo-OS installed: VPN and FlareSolverr (media stack only),
// Update and Remove.
export function AppActions({ app }: { app: CatalogApp }) {
    const queryClient = useQueryClient()
    const [confirm, setConfirm] = useState<'update' | 'remove' | null>(null)
    const [vpnOpen, setVpnOpen] = useState(false)
    const [flareSolverrOpen, setFlareSolverrOpen] = useState(false)
    const [hostname, setHostname] = useState('')

    const refresh = () => queryClient.invalidateQueries({ queryKey: appsQuery.queryKey })
    const update = useMutation({
        mutationFn: () => api.updateApp(app.id),
        onSuccess: refresh,
        onError: (error) => toast.error(`Could not update ${app.name}`, { description: error.message }),
    })
    const remove = useMutation({
        mutationFn: () => api.removeApp(app.id, app.id === 'headscale' ? hostname : undefined),
        onSuccess: refresh,
        onError: (error) => toast.error(`Could not remove ${app.name}`, { description: error.message }),
    })
    const busy = update.isPending || remove.isPending

    const recovery = useMutation({
        mutationFn: api.photosRecovery,
        onSuccess: refresh,
        onError: (e) => toast.error(e.message),
    })

    return (
        <>
            {app.id === 'photos' && (
                <>
                    <Button
                        variant="outline"
                        disabled={busy || recovery.isPending || app.status !== 'running'}
                        onClick={() => recovery.mutate('backup')}
                    >
                        Back up Photos
                    </Button>
                    <Button
                        variant="outline"
                        disabled={busy || recovery.isPending || app.status !== 'running'}
                        onClick={() => recovery.mutate('verify')}
                    >
                        Verify recovery
                    </Button>
                </>
            )}
            {app.id === 'media' && (
                <Button variant="outline" disabled={busy || app.status !== 'running'} onClick={() => setVpnOpen(true)}>
                    <ShieldIcon />
                    VPN
                </Button>
            )}
            {app.id === 'media' && app.flareSolverr !== undefined && (
                <Button
                    variant="outline"
                    disabled={busy || app.status !== 'running'}
                    onClick={() => setFlareSolverrOpen(true)}
                >
                    <CloudIcon />
                    FlareSolverr
                </Button>
            )}
            <Button
                variant={app.updateAvailable ? 'default' : 'outline'}
                disabled={busy || app.status !== 'running'}
                onClick={() => setConfirm('update')}
            >
                {update.isPending ? <Loader2Icon className="animate-spin" /> : <ArrowUpCircleIcon />}
                Update
            </Button>
            <Button variant="destructive" disabled={busy} onClick={() => setConfirm('remove')}>
                {remove.isPending ? <Loader2Icon className="animate-spin" /> : <Trash2Icon />}
                Remove
            </Button>

            <VpnDialog app={app} open={vpnOpen} onOpenChange={setVpnOpen} />
            <FlareSolverrDialog app={app} open={flareSolverrOpen} onOpenChange={setFlareSolverrOpen} />

            <AlertDialog open={confirm === 'update'} onOpenChange={(open) => !open && setConfirm(null)}>
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>Update {app.name}?</AlertDialogTitle>
                        <AlertDialogDescription className="flex flex-col gap-2">
                            <span>{updateText[app.id]}</span>
                            {app.updateAvailable && (
                                <span>
                                    The stack in the container is from{' '}
                                    {app.version ? `Iglo-OS ${app.version}` : 'an older Iglo-OS release'}.
                                </span>
                            )}
                            <span>
                                {['photos', 'music', 'inbox'].includes(app.id)
                                    ? 'Automatic disk rollback is disabled because NAS data is outside the guest. Inspect the operation and application backup before recovering.'
                                    : 'If the update fails, Iglo-OS rolls the container back to the snapshot.'}
                            </span>
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel>Cancel</AlertDialogCancel>
                        <AlertDialogAction
                            onClick={() => {
                                setConfirm(null)
                                update.mutate()
                            }}
                        >
                            Update
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>

            <AlertDialog open={confirm === 'remove'} onOpenChange={(open) => !open && setConfirm(null)}>
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>Remove {app.name}?</AlertDialogTitle>
                        <AlertDialogDescription className="flex flex-col gap-2">
                            <span>
                                This removes container {app.vmid} with its disks and snapshots. {removeText[app.id]}
                            </span>
                            <span>
                                The backups of the container stay, so you can restore it on the Backups page.
                                {app.id === 'headscale' &&
                                    ' Controller backups contain private keys. Protect them and keep an off-device recovery copy.'}
                                {['media', 'jellyfin'].includes(app.id) &&
                                    ' The Videos share stays connected. Disconnect it on the Storage page once no app uses it.'}
                            </span>
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    {app.id === 'headscale' && (
                        <div className="flex flex-col gap-2">
                            <Label htmlFor="remove-controller">
                                Type {app.controllerHost || 'the installed hostname'} to confirm
                            </Label>
                            <Input
                                id="remove-controller"
                                value={hostname}
                                onChange={(e) => setHostname(e.target.value)}
                                autoComplete="off"
                            />
                        </div>
                    )}
                    <AlertDialogFooter>
                        <AlertDialogCancel>Cancel</AlertDialogCancel>
                        <AlertDialogAction
                            disabled={
                                app.id === 'headscale' && (!app.controllerHost || hostname !== app.controllerHost)
                            }
                            variant="destructive"
                            onClick={() => {
                                setConfirm(null)
                                remove.mutate()
                            }}
                        >
                            Remove
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </>
    )
}
