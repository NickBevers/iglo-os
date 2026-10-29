import { useState, type SubmitEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { api, type InboxProfile } from '@/lib/api'
import { isLive } from '@/lib/live'
import { Field } from './form-parts'

export function InboxPage() {
    const query = useQuery({
        queryKey: ['inbox-jobs'],
        queryFn: api.inboxJobs,
        refetchInterval: (q) =>
            isLive() ? 60000 : q.state.data?.jobs.some((j) => ['queued', 'running'].includes(j.state)) ? 5000 : 30000,
    })
    const client = useQueryClient()
    const [path, setPath] = useState('')
    const refresh = () => {
        void client.invalidateQueries({ queryKey: ['inbox-jobs'] })
        void client.invalidateQueries({ queryKey: ['apps'] })
    }
    const change = useMutation({
        mutationFn: async (action: () => Promise<unknown>) => action(),
        onSuccess: refresh,
        onError: (e) => toast.error(e.message),
    })
    const view = query.data
    function submit(e: SubmitEvent) {
        e.preventDefault()
        if (view)
            change.mutate(async () => {
                await api.submitInbox({ path, profile: view.profile })
                setPath('')
            })
    }
    return (
        <div className="flex flex-col gap-6">
            <header>
                <h1 className="page-title">Inbox work</h1>
                <p className="text-sm text-muted-foreground">
                    One local video at a time. Originals stay read only. Completed files go to Videos/home-videos.
                </p>
            </header>
            {query.isPending && <p>Loading the worker…</p>}
            {query.error && (
                <p role="alert">{query.error.message}. Install Inbox from Apps and start its container first.</p>
            )}
            {view?.sample && (
                <p className="text-sm text-muted-foreground">Development simulation. No real video conversion.</p>
            )}
            {view && (
                <>
                    <Card>
                        <CardHeader>
                            <CardTitle>
                                {view.paused
                                    ? 'Paused manually'
                                    : !view.storageReady
                                      ? 'Storage unavailable'
                                      : !view.admitted
                                        ? 'Waiting for background work admission'
                                        : 'Ready to process'}
                            </CardTitle>
                        </CardHeader>
                        <CardContent className="flex flex-col gap-4">
                            <p className="text-sm text-muted-foreground">
                                With Background work in control, scans run only while work may start, at most every 30
                                minutes. Without it, they run every 6 hours. A paused queue is not scanned on a timer.
                                Scan now always works. New files must be unchanged for 30 seconds. Pause holds new jobs;
                                active work finishes. Gaming, the work window and host memory can also hold admission.
                            </p>
                            <div className="flex flex-wrap gap-3">
                                <Button
                                    variant="outline"
                                    disabled={change.isPending}
                                    onClick={() =>
                                        change.mutate(() =>
                                            api.inboxSettings({ paused: !view.paused, profile: view.profile }),
                                        )
                                    }
                                >
                                    {view.paused ? 'Resume queue' : 'Pause queue'}
                                </Button>
                                <Button
                                    variant="outline"
                                    disabled={change.isPending}
                                    onClick={() => change.mutate(api.scanInbox)}
                                >
                                    Scan Inbox now
                                </Button>
                            </div>
                            <Field id="inbox-profile" label="Profile for new jobs">
                                <select
                                    id="inbox-profile"
                                    className="rounded-md border bg-background p-2 text-sm"
                                    value={view.profile}
                                    disabled={change.isPending}
                                    onChange={(e) =>
                                        change.mutate(() =>
                                            api.inboxSettings({
                                                paused: view.paused,
                                                profile: e.target.value as InboxProfile,
                                            }),
                                        )
                                    }
                                >
                                    <option value="preserve">Original codecs</option>
                                    <option value="compatible">Playback: H.264 MP4</option>
                                </select>
                            </Field>
                            <p className="text-sm text-muted-foreground">
                                Original codecs copies video, audio and subtitles without quality loss. Playback
                                converts only incompatible streams and keeps resolution. HDR and subtitles that need
                                conversion require the original-codec profile. Queued jobs keep their chosen profile.{' '}
                                {view.gpu ? 'VAAPI encoding is available.' : 'CPU encoding is available.'}
                            </p>
                            <form onSubmit={submit} className="flex flex-col gap-3">
                                <Field id="inbox-path" label="Video path relative to Inbox">
                                    <Input
                                        id="inbox-path"
                                        required
                                        value={path}
                                        onChange={(e) => setPath(e.target.value)}
                                        placeholder="family/holiday.mov"
                                    />
                                </Field>
                                <Button className="self-start" disabled={change.isPending}>
                                    Add to queue
                                </Button>
                            </form>
                            <p className="text-xs text-muted-foreground">
                                Up to four hours and 16 GiB per video. Local staging needs the source size, 16 GiB
                                output allowance and 4 GiB free headroom. Changed files are new versions; completed
                                versions are not queued again.
                            </p>
                            {view.scanError && <p role="alert">{view.scanError}</p>}
                        </CardContent>
                    </Card>
                    {[...view.jobs].reverse().map((j) => (
                        <Card key={j.id}>
                            <CardContent className="flex flex-col gap-2 pt-6 text-sm">
                                <p className="break-all">
                                    {j.path} · {j.state} · {j.profile === 'preserve' ? 'Original codecs' : 'Playback'}
                                </p>
                                {j.file && <p className="break-all">Published: {j.file}</p>}
                                {j.error && (
                                    <p role="alert" className="break-all text-destructive">
                                        {j.error}
                                    </p>
                                )}
                                {j.state === 'failed' && (
                                    <Button
                                        variant="outline"
                                        className="self-start"
                                        disabled={change.isPending}
                                        onClick={() => change.mutate(() => api.retryInbox(j.id))}
                                    >
                                        Retry job
                                    </Button>
                                )}
                            </CardContent>
                        </Card>
                    ))}
                </>
            )}
        </div>
    )
}
