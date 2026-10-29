import { useState, type SubmitEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2Icon } from 'lucide-react'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { api, type PhotosBackup, type PhotosBackupSettings } from '@/lib/api'
import { formatBytes, formatDateTime, formatRelative } from '@/lib/format'
import { appsQuery } from '@/lib/queries'
import { Field } from './form-parts'

function repositorySize(bytes?: number, snapshots?: number) {
    if (!bytes) {
        return ''
    }
    return `${formatBytes(bytes)}, ${snapshots ?? 0} ${snapshots === 1 ? 'snapshot' : 'snapshots'}`
}

// PhotosBackupDetails shows the last Photos backup, the size of the backups on
// the NAS and off it, and the last check.
export function PhotosBackupDetails({ backup }: { backup: PhotosBackup }) {
    const [open, setOpen] = useState(false)
    const { settings, status } = backup
    const offsite = status.offsite

    let offsiteText =
        'Not set. The Photos share and the backups are on the same NAS. Add a disk of the host for a copy off it.'
    let offsiteBadge: 'success' | 'warning' | 'destructive' | 'outline' = 'outline'
    if (!settings.offsitePath) {
        offsiteBadge = 'outline'
    } else if (offsite.result === 'missing' || offsite.result === 'failed') {
        const last = offsite.finished ? ` The last copy that worked is from ${formatDateTime(offsite.finished)}.` : ''
        offsiteText = `${offsite.message ?? 'The last copy failed'}.${last}`
        offsiteBadge = offsite.result === 'missing' ? 'warning' : 'destructive'
    } else if (offsite.finished) {
        offsiteText = `${settings.offsitePath}, copied ${formatRelative(offsite.finished)}. ${repositorySize(offsite.bytes, offsite.snapshots)}`
        offsiteBadge = 'success'
    } else {
        offsiteText = `${settings.offsitePath}. The next backup makes the first copy.`
    }

    return (
        <section className="flex flex-col gap-3 rounded-md border p-4 text-sm" aria-labelledby="photos-backup-title">
            <div className="flex flex-wrap items-center justify-between gap-2">
                <h3 id="photos-backup-title" className="font-medium">
                    Backups
                </h3>
                <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
                    Backup settings
                </Button>
            </div>
            <dl className="grid gap-x-4 gap-y-2 sm:grid-cols-[max-content_1fr]">
                <dt className="text-muted-foreground">Last backup</dt>
                <dd className="flex flex-wrap items-center gap-2">
                    {status.finished ? (
                        <>
                            <Badge variant={status.result === 'ok' ? 'success' : 'destructive'}>
                                {status.result === 'ok' ? 'Worked' : 'Failed'}
                            </Badge>
                            <span>
                                {formatRelative(status.finished)}
                                {status.result === 'ok' && status.snapshot && `, snapshot ${status.snapshot}`}
                                {status.result === 'failed' && status.message && `. ${status.message}.`}
                            </span>
                        </>
                    ) : (
                        <span>None yet</span>
                    )}
                </dd>
                <dt className="text-muted-foreground">Schedule</dt>
                <dd>
                    Every night at {settings.time}. Keeps {settings.keepDaily} daily, {settings.keepWeekly} weekly and{' '}
                    {settings.keepMonthly} monthly snapshots.
                </dd>
                <dt className="text-muted-foreground">On the NAS</dt>
                <dd>{repositorySize(status.nas.bytes, status.nas.snapshots) || 'No backups yet'}</dd>
                <dt className="text-muted-foreground">Off the NAS</dt>
                <dd className="flex flex-wrap items-center gap-2">
                    {offsiteBadge !== 'outline' && (
                        <Badge variant={offsiteBadge}>
                            {offsiteBadge === 'success'
                                ? 'Copied'
                                : offsiteBadge === 'warning'
                                  ? 'Disk missing'
                                  : 'Failed'}
                        </Badge>
                    )}
                    <span>{offsiteText}</span>
                </dd>
                <dt className="text-muted-foreground">Last check</dt>
                <dd>
                    {status.verified
                        ? status.verifyResult === 'ok'
                            ? `Worked ${formatRelative(status.verified)}, snapshot ${status.verifySnapshot ?? ''}`
                            : `Failed ${formatRelative(status.verified)}. ${status.verifyMessage ?? ''}`
                        : 'Never. Run Verify recovery once the first backup is done.'}
                </dd>
            </dl>
            <p className="text-xs text-muted-foreground">
                {backup.hasPassword
                    ? 'The backups are encrypted with the backup password you typed when you installed Photos. The host keeps it sealed and never shows it. Keep it in your password manager: without it, no backup can be read after the host is lost.'
                    : "Photos has no backup password, so it can't back up. Run iglo-photos-key set as root on the host, and keep the password in your password manager."}
            </p>
            <PhotosBackupDialog settings={settings} open={open} onOpenChange={setOpen} />
        </section>
    )
}

type DialogProps = { settings: PhotosBackupSettings; open: boolean; onOpenChange: (open: boolean) => void }

function PhotosBackupDialog({ settings, open, onOpenChange }: DialogProps) {
    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-lg">
                <DialogHeader>
                    <DialogTitle>Photos backup settings</DialogTitle>
                    <DialogDescription>
                        The nightly backup waits while Proxmox backups or updates run. Photos stays online.
                    </DialogDescription>
                </DialogHeader>
                {open && <PhotosBackupForm current={settings} onDone={() => onOpenChange(false)} />}
            </DialogContent>
        </Dialog>
    )
}

function PhotosBackupForm({ current, onDone }: { current: PhotosBackupSettings; onDone: () => void }) {
    const queryClient = useQueryClient()
    const [settings, setSettings] = useState(current)
    const [error, setError] = useState('')
    const save = useMutation({
        mutationFn: () => api.savePhotosBackups(settings),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: appsQuery.queryKey })
            toast.success('The Photos backup settings are saved')
            onDone()
        },
        onError: (e) => setError(e.message),
    })
    const submit = (event: SubmitEvent) => {
        event.preventDefault()
        setError('')
        save.mutate()
    }
    const keep = (key: 'keepDaily' | 'keepWeekly' | 'keepMonthly', label: string) => (
        <Field id={`photos-${key}`} label={label}>
            <Input
                id={`photos-${key}`}
                type="number"
                min={0}
                max={500}
                required
                value={settings[key]}
                onChange={(e) => setSettings({ ...settings, [key]: Number(e.target.value) })}
            />
        </Field>
    )

    return (
        <form onSubmit={submit} className="flex flex-col gap-4">
            <Field
                id="photos-backup-time"
                label="Time of the nightly backup"
                help="01:00 runs before the processing window of background work at 02:00 and the Proxmox backups at 03:00."
            >
                <Input
                    id="photos-backup-time"
                    type="time"
                    required
                    value={settings.time}
                    onChange={(e) => setSettings({ ...settings, time: e.target.value })}
                />
            </Field>
            <div className="grid gap-4 sm:grid-cols-3">
                {keep('keepDaily', 'Daily')}
                {keep('keepWeekly', 'Weekly')}
                {keep('keepMonthly', 'Monthly')}
            </div>
            <Field
                id="photos-offsite"
                label="Folder off the NAS (optional)"
                error={error || undefined}
                help="A folder on a disk attached to the host, like /mnt/backup-disk. The host refuses the system disk and network shares, and writes .iglo-photos-disk there to know the disk again. Empty keeps the backups on the NAS only."
            >
                <Input
                    id="photos-offsite"
                    value={settings.offsitePath}
                    placeholder="/mnt/backup-disk"
                    autoComplete="off"
                    spellCheck={false}
                    onChange={(e) => setSettings({ ...settings, offsitePath: e.target.value.trim() })}
                />
            </Field>
            <DialogFooter>
                <Button type="submit" disabled={save.isPending}>
                    {save.isPending && <Loader2Icon className="animate-spin" />}
                    Save
                </Button>
            </DialogFooter>
        </form>
    )
}
