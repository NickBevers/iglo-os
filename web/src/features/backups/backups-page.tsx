import { useQuery } from '@tanstack/react-query'
import { CircleCheckIcon, CircleDashedIcon, CircleXIcon, Loader2Icon, TriangleAlertIcon } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import type { BackupRun, Backups } from '@/lib/api'
import { formatDateTime, formatRelative } from '@/lib/format'
import { backupsQuery } from '@/lib/queries'
import { GuestBackups } from './guest-backups'
import { JobCard } from './job-card'

export function BackupsPage() {
    const { data, error, isPending } = useQuery(backupsQuery)

    if (isPending) {
        return (
            <div className="flex flex-col gap-6">
                <Skeleton className="h-10 w-64" />
                <Skeleton className="h-64 w-full rounded-xl" />
            </div>
        )
    }

    if (error) {
        return (
            <Alert variant="destructive">
                <TriangleAlertIcon />
                <AlertTitle>Could not load backups</AlertTitle>
                <AlertDescription>{error.message}</AlertDescription>
            </Alert>
        )
    }

    const { job } = data

    return (
        <div className="flex flex-col gap-8">
            <header className="flex flex-wrap items-center justify-between gap-3">
                <div>
                    <h1 className="page-title">Backups</h1>
                    <p className="text-sm text-muted-foreground">
                        {!job.exists
                            ? 'No backup schedule yet'
                            : !job.enabled
                              ? 'Automatic backups are off'
                              : job.nextRun
                                ? `Next backup: ${formatDateTime(job.nextRun)} on ${job.storage}`
                                : `Backups go to ${job.storage}`}
                    </p>
                </div>
                {data.busy && (
                    <Badge variant="secondary">
                        <Loader2Icon className="animate-spin" />
                        {data.busy}…
                    </Badge>
                )}
            </header>

            <section className="flex flex-col gap-3">
                <h2 className="section-title">Containers and VMs</h2>
                <GuestBackups backups={data} />
            </section>

            <RestoreCheck check={data.check} />

            <JobCard key={JSON.stringify(job)} backups={data} />

            <section className="flex flex-col gap-3">
                <h2 className="section-title">History</h2>
                <BackupHistory runs={data.history} />
            </section>
        </div>
    )
}

// Include the date so a postponed check window is clear.
const checkDate = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' })

// RestoreCheck shows the last result and the next nightly window. The
// newest backup of the next container goes to a copy that starts once.
function RestoreCheck({ check }: { check: Backups['check'] }) {
    const last = check.last
    return (
        <Card className="flex flex-row items-start gap-3 p-4 text-sm">
            {!last ? (
                <CircleDashedIcon className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden />
            ) : last.succeeded ? (
                <CircleCheckIcon className="mt-0.5 size-4 shrink-0 text-success" aria-label="succeeded" />
            ) : (
                <CircleXIcon className="mt-0.5 size-4 shrink-0 text-destructive" aria-label="failed" />
            )}
            <div className="flex flex-col gap-1">
                <span className="font-medium">
                    {!last
                        ? 'No restore check yet'
                        : `${last.succeeded ? 'Restore check passed' : 'Restore check failed'}: ${last.target}, ${formatRelative(last.finishedAt)}`}
                </span>
                <span className="text-muted-foreground">
                    {last && !last.succeeded ? `${last.message}. ` : ''}
                    Each container is due every 30 days. At most one overdue container is checked each night in a copy
                    without network, NAS shares or devices. Privileged containers are refused. Next window:{' '}
                    {checkDate.format(new Date(check.next))}. Test a backup any time with Test.
                </span>
            </div>
        </Card>
    )
}

function BackupHistory({ runs }: { runs: BackupRun[] }) {
    if (runs.length === 0) {
        return (
            <Card className="p-6 text-center text-sm text-muted-foreground">
                Backups, restores, restore checks and deletes show up here, also the backups of the nightly job. Set up
                ntfy on the Updates page to hear when one fails.
            </Card>
        )
    }

    return (
        <Card className="gap-0 py-0">
            <ul className="divide-y">
                {runs.map((run) => (
                    <li key={run.id} className="flex items-center gap-3 px-4 py-3 text-sm">
                        {run.succeeded ? (
                            <CircleCheckIcon className="size-4 shrink-0 text-success" aria-label="succeeded" />
                        ) : (
                            <CircleXIcon className="size-4 shrink-0 text-destructive" aria-label="failed" />
                        )}
                        <div className="min-w-0 flex-1">
                            <span className="font-medium">{run.target}</span>
                            {run.scheduled && <span className="ml-2 text-xs text-muted-foreground">Scheduled</span>}
                            <p className="text-muted-foreground">{run.message}</p>
                        </div>
                        <span className="hidden text-muted-foreground sm:inline">{formatRelative(run.finishedAt)}</span>
                    </li>
                ))}
            </ul>
        </Card>
    )
}
