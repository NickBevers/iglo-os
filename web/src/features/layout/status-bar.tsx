import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'

import { SidebarTrigger } from '@/components/ui/sidebar'
import { healthProblems } from '@/lib/health'
import { healthQuery, overviewQuery, selfUpdateQuery } from '@/lib/queries'
import { cn } from '@/lib/utils'

type Tone = 'ok' | 'warning' | 'critical' | 'neutral'

const dots: Record<Tone, string> = {
    ok: 'bg-success',
    warning: 'bg-warning',
    critical: 'bg-destructive',
    neutral: 'bg-muted-foreground/50',
}

// StatusBar is the bar above every page: where you are, and what needs you.
export function StatusBar({ page }: { page?: string }) {
    const { data: overview } = useQuery(overviewQuery)
    const { data: health } = useQuery(healthQuery)
    const { data: selfUpdate } = useQuery(selfUpdateQuery)

    const node = overview?.nodes[0]
    const problems = health ? healthProblems(health).length : 0

    return (
        <header className="sticky top-0 z-10 flex h-12 items-center gap-2 border-b bg-background/85 px-3 backdrop-blur-md sm:px-4">
            <SidebarTrigger className="-ml-1" />
            <span className="mx-1 h-4 w-px shrink-0 bg-border" aria-hidden="true" />
            <nav aria-label="Where you are" className="flex min-w-0 items-center gap-1.5 text-sm">
                <Link to="/" className="truncate text-muted-foreground hover:text-foreground">
                    {node?.name ?? 'Iglo-OS'}
                </Link>
                {page && (
                    <>
                        <span className="text-muted-foreground/60" aria-hidden="true">
                            /
                        </span>
                        <span className="truncate font-medium">{page}</span>
                    </>
                )}
            </nav>

            <div className="ml-auto flex items-center gap-1.5">
                {selfUpdate?.updateAvailable && (
                    <Chip to="/updates" tone="ok" className="hidden sm:inline-flex">
                        Iglo-OS {selfUpdate.latest?.version} is out
                    </Chip>
                )}
                {health && (
                    <Chip to="/health" tone={problems > 0 ? 'critical' : 'ok'}>
                        {problems === 0 ? 'Healthy' : problems === 1 ? '1 problem' : `${problems} problems`}
                    </Chip>
                )}
                {overview && (
                    <Chip to="/" tone={overview.agent.connected ? 'ok' : 'critical'} className="hidden md:inline-flex">
                        {overview.agent.connected ? 'Agent connected' : 'Agent offline'}
                    </Chip>
                )}
                {node && (
                    <Chip to="/" tone={node.status === 'online' ? 'ok' : 'critical'} className="hidden md:inline-flex">
                        Host {node.status}
                    </Chip>
                )}
            </div>
        </header>
    )
}

function Chip({ to, tone, className, children }: { to: string; tone: Tone; className?: string; children: ReactNode }) {
    return (
        <Link
            to={to}
            className={cn(
                'inline-flex h-6 items-center gap-1.5 rounded-full border bg-card px-2.5 text-xs whitespace-nowrap text-foreground/85 transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none',
                className,
            )}
        >
            <span className={cn('size-1.5 rounded-full', dots[tone])} aria-hidden="true" />
            {children}
        </Link>
    )
}
