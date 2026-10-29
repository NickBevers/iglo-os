import { useQuery } from '@tanstack/react-query'
import { CircleCheckIcon, Loader2Icon, TriangleAlertIcon } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import type { SelfUpdate } from '@/lib/api'
import { formatRelative } from '@/lib/format'
import { selfUpdateQuery } from '@/lib/queries'

// SelfUpdateCard shows the running version and how the last upgrade went.
// Upgrades come from "make deploy" on your own machine, not from this page.
export function SelfUpdateCard() {
    const { data, error } = useQuery(selfUpdateQuery)

    if (!data) {
        if (error) {
            return null
        }
        return <Skeleton className="h-32 w-full rounded-xl" />
    }

    return (
        <Card>
            <CardHeader>
                <CardTitle className="flex items-center gap-2">
                    Iglo-OS
                    <Badge variant="outline" className="font-mono">
                        {data.version}
                    </Badge>
                </CardTitle>
                <CardDescription>
                    New versions come from <code>make deploy</code> on your own machine. See docs/development.md.
                </CardDescription>
            </CardHeader>
            {data.upgrade && (
                <CardContent>
                    <UpgradeState view={data} />
                </CardContent>
            )}
        </Card>
    )
}

function UpgradeState({ view }: { view: SelfUpdate }) {
    const { upgrade } = view
    if (!upgrade) {
        return null
    }

    if (upgrade.state === 'running') {
        const restarted = upgrade.version === view.version
        return (
            <Alert role="status">
                <Loader2Icon className="animate-spin" />
                <AlertTitle>
                    {restarted ? `Making sure that ${upgrade.version} works…` : `Installing ${upgrade.version}…`}
                </AlertTitle>
                <AlertDescription>
                    {restarted
                        ? 'If it does not work, the previous version comes back.'
                        : 'The manager restarts. This page reloads when the new version runs.'}
                </AlertDescription>
            </Alert>
        )
    }

    if (upgrade.state === 'failed' && upgrade.version !== view.version) {
        return (
            <Alert variant="destructive">
                <TriangleAlertIcon />
                <AlertTitle>Installing {upgrade.version} failed</AlertTitle>
                <AlertDescription className="flex flex-col gap-2">
                    <span>{upgrade.message}</span>
                    {upgrade.log && (
                        <details>
                            <summary className="cursor-pointer">Show log</summary>
                            <pre className="mt-2 max-h-64 overflow-auto rounded-md bg-muted p-3 font-mono text-xs whitespace-pre-wrap text-foreground">
                                {upgrade.log}
                            </pre>
                        </details>
                    )}
                </AlertDescription>
            </Alert>
        )
    }

    if (upgrade.state === 'succeeded' && upgrade.version === view.version) {
        return (
            <p className="flex items-center gap-2 text-sm text-muted-foreground">
                <CircleCheckIcon className="size-4 text-success" />
                Upgraded to {upgrade.version}
                {upgrade.finishedAt && ` ${formatRelative(upgrade.finishedAt)}`}
            </p>
        )
    }

    return null
}
