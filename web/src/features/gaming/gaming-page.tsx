import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { AlertTriangleIcon, Gamepad2Icon } from 'lucide-react'
import { toast } from 'sonner'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { api } from '@/lib/api'
import { formatDateTime } from '@/lib/format'
import { GameLibrary } from './game-library'

export function GamingPage() {
    const query = useQuery({ queryKey: ['gaming'], queryFn: api.gaming, refetchInterval: 5000 })
    const client = useQueryClient()
    const play = useMutation({
        mutationFn: api.playGaming,
        onSuccess: (_, playing) => {
            client.invalidateQueries({ queryKey: ['gaming'] })
            client.invalidateQueries({ queryKey: ['workloads'] })
            toast.success(playing ? 'Starting the gaming container' : 'Shutting down the gaming container')
        },
        onError: (e) => toast.error(e.message),
    })

    return (
        <div className="flex flex-col gap-6">
            <header className="flex flex-col gap-1">
                <div className="flex items-center gap-2">
                    <h1 className="page-title">Gaming</h1>
                    <Badge variant="outline">Experimental</Badge>
                </div>
                <p className="text-sm text-muted-foreground">
                    Retro games from your Games share, streamed to your PC or TV with Moonlight.
                </p>
            </header>
            {query.isPending && <p>Loading gaming…</p>}
            {query.error && (
                <Alert variant="destructive">
                    <AlertTriangleIcon />
                    <AlertTitle>Could not load gaming</AlertTitle>
                    <AlertDescription>{query.error.message}</AlertDescription>
                </Alert>
            )}
            {query.data && !query.data.installed && (
                <Card>
                    <CardHeader>
                        <CardTitle>Gaming is not installed</CardTitle>
                        <CardDescription>
                            Connect a Games share in{' '}
                            <Link to="/storage" className="underline">
                                Storage
                            </Link>{' '}
                            with roms and bios folders, then install Gaming from{' '}
                            <Link to="/apps" className="underline">
                                Apps
                            </Link>
                            .
                        </CardDescription>
                    </CardHeader>
                </Card>
            )}
            {query.data?.installed && (
                <>
                    {query.data.sample && (
                        <p className="text-sm text-muted-foreground">
                            Development simulation. No container starts and the library is sample data.
                        </p>
                    )}
                    <Card>
                        <CardHeader>
                            <CardTitle>{query.data.status === 'running' ? 'Ready to play' : 'Stopped'}</CardTitle>
                            <CardDescription>
                                {query.data.status === 'running'
                                    ? query.data.address
                                        ? `Add ${query.data.address} as a host in Moonlight, then choose Retro games.`
                                        : 'The container runs. Its address shows once it has one.'
                                    : 'The container only runs while you play, so it uses no RAM or GPU now.'}
                            </CardDescription>
                        </CardHeader>
                        <CardContent className="flex flex-col gap-2 text-sm text-muted-foreground">
                            <p>
                                {query.data.protected && query.data.protectedUntil
                                    ? `Heavy background work waits until you stop, or until ${formatDateTime(query.data.protectedUntil)} at the latest.`
                                    : 'Play also holds heavy background work, like photo analysis and video conversion, for up to 8 hours.'}
                            </p>
                            {query.data.status === 'running' && (
                                <p>
                                    Save in the game and quit the emulator before you stop. Stop shuts the container
                                    down.
                                </p>
                            )}
                        </CardContent>
                        <CardFooter>
                            {query.data.status === 'running' ? (
                                <Button variant="outline" disabled={play.isPending} onClick={() => play.mutate(false)}>
                                    Stop
                                </Button>
                            ) : (
                                <Button disabled={play.isPending} onClick={() => play.mutate(true)}>
                                    <Gamepad2Icon />
                                    Play
                                </Button>
                            )}
                        </CardFooter>
                    </Card>
                    <GameLibrary />
                    <Card>
                        <CardHeader>
                            <CardTitle>First time</CardTitle>
                        </CardHeader>
                        <CardContent>
                            <ol className="flex list-decimal flex-col gap-2 pl-5 text-sm text-muted-foreground">
                                <li>Install Moonlight on your PC or on the TV box.</li>
                                <li>Press Play here, then add the address above as a host in Moonlight.</li>
                                <li>
                                    Moonlight shows a PIN. Wolf's log in{' '}
                                    <Link to="/logs" className="underline">
                                        Logs
                                    </Link>{' '}
                                    has the pairing link for it. Pair only devices you know.
                                </li>
                                <li>Choose Retro games. Every screen uses the same saves, so play on one at a time.</li>
                            </ol>
                        </CardContent>
                    </Card>
                </>
            )}
        </div>
    )
}
