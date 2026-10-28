import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { api } from '@/lib/api'
import { isLive } from '@/lib/live'
import { Field } from './form-parts'

export function MusicPage() {
    // Each check asks the worker in its container, so poll fast only while jobs wait or run.
    // The live stream says when a job changes, so then the poll is only a fallback.
    const query = useQuery({
        queryKey: ['music-jobs'],
        queryFn: api.musicJobs,
        refetchInterval: (query) =>
            isLive()
                ? 60000
                : query.state.data?.jobs.some((job) => job.state === 'queued' || job.state === 'running')
                  ? 5000
                  : 30000,
    })
    const client = useQueryClient()
    const [url, setURL] = useState('')
    const [video, setVideo] = useState(false)
    const refresh = () => client.invalidateQueries({ queryKey: ['music-jobs'] })
    const submit = useMutation({
        mutationFn: () => api.submitMusic({ url, video }),
        onSuccess: () => {
            setURL('')
            refresh()
            toast.success('Added to the music queue')
        },
        onError: (e) => toast.error(e.message),
    })
    const pause = useMutation({
        mutationFn: api.pauseMusic,
        onSuccess: refresh,
        onError: (e) => toast.error(e.message),
    })
    function send(e: FormEvent) {
        e.preventDefault()
        submit.mutate()
    }
    return (
        <div className="flex flex-col gap-6">
            <header>
                <h1 className="page-title">Music work</h1>
                <p className="text-sm text-muted-foreground">
                    One download at a time, with metadata and cover art. Add files to your Jellyfin library after
                    publication.
                </p>
            </header>
            {query.isPending && <p>Loading the worker…</p>}
            {query.error && (
                <p role="alert">{query.error.message}. Install Music from Apps and start its container first.</p>
            )}
            {query.data && (
                <>
                    {query.data.sample && (
                        <p className="text-sm text-muted-foreground">Development simulation. No media is downloaded.</p>
                    )}
                    <Card>
                        <CardHeader>
                            <CardTitle>
                                {query.data.paused
                                    ? 'Paused manually'
                                    : query.data.storageReady === false
                                      ? 'Music share unavailable'
                                      : !query.data.admitted
                                        ? 'Waiting for processing admission'
                                        : 'Ready to process'}
                            </CardTitle>
                        </CardHeader>
                        <CardContent className="flex flex-col gap-4">
                            <p className="text-sm text-muted-foreground">
                                Pause defers new jobs. Active downloads finish. Background work can also hold admission
                                for gaming, the daily window or host RAM.
                            </p>
                            <Button
                                variant="outline"
                                className="self-start"
                                disabled={pause.isPending}
                                onClick={() => pause.mutate(!query.data.paused)}
                            >
                                {query.data.paused ? 'Resume queue' : 'Pause queue'}
                            </Button>
                            <form onSubmit={send} className="flex flex-col gap-4">
                                <Field id="music-url" label="YouTube or YouTube Music video URL">
                                    <Input
                                        id="music-url"
                                        type="url"
                                        required
                                        value={url}
                                        onChange={(e) => setURL(e.target.value)}
                                        placeholder="https://music.youtube.com/watch?v=…"
                                    />
                                </Field>
                                <label className="flex items-center gap-3 text-sm">
                                    <Switch checked={video} onCheckedChange={setVideo} />
                                    Include video (up to 1080p)
                                </label>
                                <p className="text-xs text-muted-foreground">
                                    Single videos up to 20 minutes and a 512 MiB final file. Playlists are not imported.
                                </p>
                                <Button className="self-start" disabled={submit.isPending}>
                                    Add to queue
                                </Button>
                            </form>
                        </CardContent>
                    </Card>
                    {[...query.data.jobs].reverse().map((j) => (
                        <Card key={j.id}>
                            <CardContent className="flex flex-col gap-2 pt-6 text-sm">
                                <p>
                                    {j.video ? 'Video' : 'Audio'} · {j.state}
                                </p>
                                <a className="break-all underline" href={j.url} target="_blank" rel="noreferrer">
                                    {j.url}
                                </a>
                                {j.file && <p>Published: {j.file}</p>}
                                {j.error && (
                                    <p role="alert" className="break-all whitespace-pre-wrap text-destructive">
                                        {j.error}
                                    </p>
                                )}
                            </CardContent>
                        </Card>
                    ))}
                </>
            )}
        </div>
    )
}
