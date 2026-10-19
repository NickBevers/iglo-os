import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ChevronRightIcon } from 'lucide-react'

import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { formatDateTime, formatRelative } from '@/lib/format'
import { healthProblems } from '@/lib/health'
import { backupsQuery, healthQuery, updatesQuery } from '@/lib/queries'
import { cn } from '@/lib/utils'

type Tone = 'ok' | 'warning' | 'critical' | 'neutral'

const dots: Record<Tone, string> = {
    ok: 'bg-success',
    warning: 'bg-warning',
    critical: 'bg-destructive',
    neutral: 'bg-muted-foreground/40',
}

// Summaries are the state of health, updates and backups, each with a link to its page.
export function Summaries() {
    return (
        <div className="grid gap-3 md:grid-cols-3">
            <HealthSummary />
            <UpdatesSummary />
            <BackupsSummary />
        </div>
    )
}

function HealthSummary() {
    const { data } = useQuery(healthQuery)
    if (!data) {
        return <Skeleton className="h-28 rounded-lg" />
    }

    const problems = healthProblems(data)
    return (
        <Summary
            to="/health"
            title="Health"
            tone={problems.length > 0 ? 'critical' : data.checkedAt ? 'ok' : 'neutral'}
            headline={
                problems.length === 0
                    ? data.checkedAt
                        ? 'Everything is healthy'
                        : 'Checking disks'
                    : problems.length === 1
                      ? '1 problem'
                      : `${problems.length} problems`
            }
            footer={data.checkedAt ? `Checked ${formatRelative(data.checkedAt)}` : undefined}
        >
            {problems.length > 0 ? (
                <ul className="flex flex-col gap-0.5">
                    {problems.slice(0, 2).map((problem) => (
                        <li key={problem} className="truncate">
                            {problem}
                        </li>
                    ))}
                    {problems.length > 2 && <li>and {problems.length - 2} more</li>}
                </ul>
            ) : (
                <p>
                    {data.disks.length} {data.disks.length === 1 ? 'disk' : 'disks'}, {data.storage.length} storage,{' '}
                    {data.services.length} watched {data.services.length === 1 ? 'service' : 'services'}
                </p>
            )}
        </Summary>
    )
}

function UpdatesSummary() {
    const { data } = useQuery(updatesQuery)
    if (!data) {
        return <Skeleton className="h-28 rounded-lg" />
    }

    const targets = [data.host, ...data.guests]
    const waiting = targets.reduce((count, target) => count + target.packages.length + target.images.length, 0)
    const checked = targets.some((target) => target.checkedAt)
    const reboot = data.host.rebootRequired

    return (
        <Summary
            to="/updates"
            title="Updates"
            tone={reboot ? 'warning' : !checked ? 'neutral' : waiting > 0 ? 'warning' : 'ok'}
            headline={!checked ? 'Not checked yet' : waiting === 0 ? 'Up to date' : `${waiting} waiting`}
            footer={data.nextRun ? `Next run ${formatDateTime(data.nextRun)}` : 'Automatic updates are off'}
        >
            <p>
                {reboot
                    ? 'The host needs a reboot for its new kernel.'
                    : `${data.guests.filter((guest) => guest.autoUpdate).length} of ${data.guests.length} containers update by themselves`}
            </p>
        </Summary>
    )
}

function BackupsSummary() {
    const { data } = useQuery(backupsQuery)
    if (!data) {
        return <Skeleton className="h-28 rounded-lg" />
    }

    const last = data.history.find((run) => run.kind === 'backup')
    const included = data.guests.filter((guest) => guest.included).length

    return (
        <Summary
            to="/backups"
            title="Backups"
            tone={!data.job.exists ? 'warning' : last && !last.succeeded ? 'critical' : 'ok'}
            headline={
                !data.job.exists
                    ? 'No backup job'
                    : last
                      ? `Last ${last.succeeded ? 'backup worked' : 'backup failed'}`
                      : 'No backups yet'
            }
            footer={
                data.job.exists && data.job.nextRun
                    ? `Next ${formatDateTime(data.job.nextRun)}, to ${data.job.storage}`
                    : undefined
            }
        >
            <p>
                {last ? `${last.target} ${formatRelative(last.finishedAt)}. ` : ''}
                {included} of {data.guests.length} guests included
            </p>
        </Summary>
    )
}

type SummaryProps = {
    to: string
    title: string
    tone: Tone
    headline: string
    footer?: string
    children: ReactNode
}

function Summary({ to, title, tone, headline, footer, children }: SummaryProps) {
    return (
        <Card className="relative gap-0 py-0 transition-colors has-[a:hover]:bg-accent/30">
            <div className="flex flex-col gap-1.5 px-4 py-3.5">
                <Link
                    to={to}
                    className="flex items-center gap-1 text-xs text-muted-foreground after:absolute after:inset-0 focus-visible:outline-none focus-visible:after:ring-2 focus-visible:after:ring-ring focus-visible:after:ring-inset"
                >
                    {title}
                    <ChevronRightIcon className="size-3" aria-hidden="true" />
                </Link>
                <p className="flex items-center gap-2 text-base font-semibold wide">
                    <span className={cn('size-2 shrink-0 rounded-full', dots[tone])} aria-hidden="true" />
                    {headline}
                </p>
                <div className="min-w-0 text-sm text-muted-foreground">{children}</div>
                {footer && <p className="text-xs text-muted-foreground/80">{footer}</p>}
            </div>
        </Card>
    )
}
