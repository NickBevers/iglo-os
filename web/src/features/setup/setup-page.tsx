import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { AlertTriangleIcon, CheckIcon, CircleDashedIcon, Loader2Icon, SkipForwardIcon, XIcon } from 'lucide-react'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { api, type Setup, type SetupStep } from '@/lib/api'
import { isLive } from '@/lib/live'
import { appsQuery } from '@/lib/queries'
import { Wizard } from './wizard'

const setupQuery = {
    queryKey: ['setup'],
    queryFn: api.setup,
    // The manager says when a step moves on; the poll covers a dropped stream.
    refetchInterval: (query: { state: { data?: Setup } }) =>
        !isLive() && query.state.data?.state === 'running' ? 3000 : 60000,
}

export function SetupPage() {
    const query = useQuery(setupQuery)

    return (
        <div className="flex flex-col gap-6">
            <header className="flex flex-col gap-1">
                <h1 className="page-title">Setup</h1>
                <p className="text-sm text-muted-foreground">
                    Install the whole stack in one go: the shares on your NAS, backups, and the apps, connected to each
                    other.
                </p>
            </header>
            {query.isPending && <p>Loading the setup…</p>}
            {query.error && (
                <Alert variant="destructive">
                    <AlertTriangleIcon />
                    <AlertTitle>Could not load the setup</AlertTitle>
                    <AlertDescription>{query.error.message}</AlertDescription>
                </Alert>
            )}
            {query.data &&
                (query.data.state === 'idle' ? <Wizard setup={query.data} /> : <Progress setup={query.data} />)}
        </div>
    )
}

const stepIcons: Record<SetupStep['state'], React.ReactNode> = {
    waiting: <CircleDashedIcon className="text-muted-foreground" />,
    running: <Loader2Icon className="animate-spin text-brand" />,
    done: <CheckIcon className="text-success" />,
    failed: <XIcon className="text-destructive" />,
    skipped: <SkipForwardIcon className="text-muted-foreground" />,
}

function Progress({ setup }: { setup: Setup }) {
    const client = useQueryClient()
    const apps = useQuery(appsQuery)
    const refresh = () => client.invalidateQueries({ queryKey: ['setup'] })
    const act = useMutation({
        mutationFn: (action: 'retry' | 'skip' | 'cancel') =>
            action === 'retry' ? api.retrySetup() : action === 'skip' ? api.skipSetup() : api.cancelSetup(),
        onSuccess: refresh,
        onError: (error) => toast.error(error.message),
    })
    const running = setup.steps.find((step) => step.state === 'running')
    const log = running?.app && apps.data?.apps.find((app) => app.id === running.app)?.operation?.log

    return (
        <Card>
            <CardHeader>
                <CardTitle>
                    {setup.state === 'done'
                        ? 'The setup is done'
                        : setup.state === 'paused'
                          ? 'The setup stopped at a step'
                          : 'Setting up'}
                </CardTitle>
                <CardDescription>
                    {setup.state === 'running' &&
                        'Each app takes a few minutes. You can close this page; the setup goes on.'}
                    {setup.state === 'paused' &&
                        'Fix the problem and try the step again, or skip it and go on. What is installed stays.'}
                    {setup.state === 'done' && (
                        <>
                            Open the apps from{' '}
                            <Link to="/apps" className="underline">
                                Apps
                            </Link>
                            . Install a single app later from there too.
                        </>
                    )}
                </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
                <ol className="flex flex-col gap-2">
                    {setup.steps.map((step) => (
                        <li key={step.id} className="flex gap-3 text-sm [&>svg]:mt-0.5 [&>svg]:size-4 [&>svg]:shrink-0">
                            {stepIcons[step.state]}
                            <span className="flex flex-col">
                                <span className={step.state === 'waiting' ? 'text-muted-foreground' : undefined}>
                                    {step.title}
                                </span>
                                {step.message && (
                                    <span
                                        className={
                                            step.state === 'failed'
                                                ? 'text-destructive'
                                                : 'text-xs text-muted-foreground'
                                        }
                                    >
                                        {step.message}
                                    </span>
                                )}
                            </span>
                        </li>
                    ))}
                </ol>
                {log && (
                    <pre className="max-h-64 overflow-auto rounded-md bg-muted p-3 font-mono text-xs whitespace-pre-wrap">
                        {log}
                    </pre>
                )}
            </CardContent>
            {setup.state !== 'running' && (
                <CardFooter className="gap-2">
                    {setup.state === 'paused' && (
                        <>
                            <Button disabled={act.isPending} onClick={() => act.mutate('retry')}>
                                Try again
                            </Button>
                            <Button variant="outline" disabled={act.isPending} onClick={() => act.mutate('skip')}>
                                Skip this step
                            </Button>
                        </>
                    )}
                    <Button variant="outline" disabled={act.isPending} onClick={() => act.mutate('cancel')}>
                        {setup.state === 'done' ? 'Start a new setup' : 'Stop the setup'}
                    </Button>
                </CardFooter>
            )}
        </Card>
    )
}
