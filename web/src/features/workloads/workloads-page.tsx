import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangleIcon } from 'lucide-react'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { api, type Workloads, type WorkloadSettings } from '@/lib/api'
import { formatBytes, formatDateTime } from '@/lib/format'
import { overviewQuery, workloadsQuery } from '@/lib/queries'

const queueNames: Record<string, string> = {
    videoConversion: 'Video conversion',
    faceDetection: 'Face detection',
    facialRecognition: 'Face recognition',
    smartSearch: 'Smart search',
    duplicateDetection: 'Duplicate detection',
    ocr: 'Text recognition',
}

export function WorkloadsPage() {
    const query = useQuery(workloadsQuery)
    const client = useQueryClient()
    const protect = useMutation({
        mutationFn: api.protectWorkloads,
        onSuccess: () => client.invalidateQueries({ queryKey: workloadsQuery.queryKey }),
        onError: (error) => toast.error(error.message),
    })
    if (query.isPending) return <Skeleton className="h-64 w-full rounded-lg" />
    if (query.error)
        return (
            <Alert variant="destructive">
                <AlertTriangleIcon />
                <AlertTitle>Could not load background work</AlertTitle>
                <AlertDescription>{query.error.message}</AlertDescription>
            </Alert>
        )
    const data = query.data
    const protecting = data.gamingActive
    const selected = data.queues.filter((queue) => data.settings.queues.includes(queue.name) || queue.owned)
    return (
        <div className="flex flex-col gap-6">
            <header className="flex flex-col gap-1">
                <h1 className="page-title">Background work</h1>
                <p className="text-sm text-muted-foreground">
                    Keep room for playback and gaming by deferring heavy photo, music and video jobs.
                </p>
            </header>
            {data.error && (
                <Alert variant="destructive">
                    <AlertTriangleIcon />
                    <AlertTitle>Control needs attention</AlertTitle>
                    <AlertDescription>
                        {data.error} The last check was{' '}
                        {data.checkedAt ? formatDateTime(data.checkedAt) : 'not completed'}.
                    </AlertDescription>
                </Alert>
            )}
            <Card>
                <CardHeader>
                    <CardTitle>{data.reason}</CardTitle>
                    <CardDescription>
                        {data.totalBytes > 0
                            ? `${formatBytes(data.freeBytes)} host headroom of ${formatBytes(data.totalBytes)}. `
                            : ''}
                        {protecting
                            ? `Gaming protection ends ${formatDateTime(data.gamingUntil!)}.`
                            : 'Use a timed hold before starting a game.'}
                    </CardDescription>
                </CardHeader>
                <CardContent className="flex flex-col gap-3">
                    <p className="text-sm text-muted-foreground">
                        Music and Inbox: {data.workerReason || 'Native and manual worker controls'}. FileFlows needs the
                        supplied pre-execute script configured in its own interface.
                    </p>
                    {data.workerError && (
                        <p role="alert" className="text-sm text-destructive">
                            Worker control: {data.workerError}
                        </p>
                    )}
                    <p className="text-sm text-muted-foreground">
                        Selected Immich queues stop accepting new work. Active jobs finish; photo uploads, thumbnails
                        and database backups keep their own settings.
                    </p>
                    <p className="text-sm text-muted-foreground">
                        This controls queue admission. It does not reserve GPU capacity, cap RAM or automatically detect
                        a game session.
                    </p>
                </CardContent>
                <CardFooter className="flex flex-wrap gap-2">
                    <Button
                        disabled={protect.isPending || (!data.settings.enabled && !data.settings.workers)}
                        onClick={() => protect.mutate(120)}
                    >
                        Protect gaming for 2 hours
                    </Button>
                    <Button
                        variant="outline"
                        disabled={protect.isPending || !protecting}
                        onClick={() => protect.mutate(0)}
                    >
                        End protection
                    </Button>
                </CardFooter>
            </Card>
            <section className="flex flex-col gap-3" aria-labelledby="queues-title">
                <h2 className="section-title" id="queues-title">
                    Heavy queues
                </h2>
                {selected.length === 0 ? (
                    <p className="text-sm text-muted-foreground">
                        Connect Immich to see its queues. Older servers without the queues API need upgrading before
                        control is available.
                    </p>
                ) : (
                    selected.map((queue) => (
                        <Card key={queue.name}>
                            <CardContent className="flex flex-wrap items-center justify-between gap-3">
                                <div>
                                    <p className="font-medium">{queueNames[queue.name] ?? queue.name}</p>
                                    <p className="text-sm text-muted-foreground">
                                        {queue.statistics.active} active ·{' '}
                                        {queue.statistics.waiting + queue.statistics.paused} waiting ·{' '}
                                        {queue.statistics.failed} failed
                                    </p>
                                </div>
                                <span className="text-sm">
                                    {queue.isPaused
                                        ? queue.owned
                                            ? 'Deferred by Iglo-OS'
                                            : 'Paused in Immich'
                                        : 'Accepting work'}
                                </span>
                            </CardContent>
                        </Card>
                    ))
                )}
            </section>
            <SettingsForm key={JSON.stringify(data.settings)} data={data} />
            <Card>
                <CardHeader>
                    <CardTitle>Other workloads</CardTitle>
                    <CardDescription>
                        FileFlows, music imports and backup maintenance are not controlled by this connection. Use their
                        native schedules for now. A shared GPU does not have a per-app reservation here.
                    </CardDescription>
                </CardHeader>
            </Card>
        </div>
    )
}

function SettingsForm({ data }: { data: Workloads }) {
    const [settings, setSettings] = useState<WorkloadSettings>(data.settings)
    const [key, setKey] = useState('')
    const overview = useQuery(overviewQuery)
    const client = useQueryClient()
    const nodes = Object.fromEntries((overview.data?.nodes ?? []).map((node) => [node.name, node.name]))
    const save = useMutation({
        mutationFn: () => api.saveWorkloadSettings({ ...settings, apiKey: key }),
        onSuccess: () => {
            setKey('')
            toast.success('Background-work settings saved')
            client.invalidateQueries({ queryKey: workloadsQuery.queryKey })
        },
        onError: (error) => toast.error(error.message),
    })
    function set<K extends keyof WorkloadSettings>(name: K, value: WorkloadSettings[K]) {
        setSettings((current) => ({ ...current, [name]: value }))
    }
    function submit(event: FormEvent) {
        event.preventDefault()
        save.mutate()
    }
    return (
        <Card>
            <form className="flex flex-col gap-6" onSubmit={submit}>
                <CardHeader>
                    <CardTitle>Processing controls</CardTitle>
                    <CardDescription>
                        Use an admin API key with queue read and update access. A blank key keeps the stored key.
                        Disable control to resume queues paused by Iglo-OS.
                    </CardDescription>
                </CardHeader>
                <CardContent className="flex flex-col gap-5">
                    <div className="flex items-center justify-between gap-3">
                        <Label htmlFor="workers-enabled">Control Music and Inbox admission</Label>
                        <Switch
                            id="workers-enabled"
                            checked={settings.workers ?? false}
                            onCheckedChange={(value) => set('workers', value)}
                        />
                    </div>
                    <p className="text-xs text-muted-foreground">
                        Uses the selected host, headroom and daily window below. A missing manager heartbeat defers new
                        work. This works independently of the Immich connection. Active jobs finish.
                    </p>
                    <div className="flex items-center justify-between gap-3">
                        <Label htmlFor="workload-enabled">Control heavy queues</Label>
                        <Switch
                            id="workload-enabled"
                            checked={settings.enabled}
                            onCheckedChange={(value) => set('enabled', value)}
                        />
                    </div>
                    <div className="grid gap-4 sm:grid-cols-2">
                        <Field label="Immich server URL" id="workload-url">
                            <Input
                                id="workload-url"
                                type="url"
                                value={settings.url}
                                onChange={(e) => set('url', e.target.value)}
                                placeholder="http://immich:2283"
                            />
                        </Field>
                        <Field label="API key" id="workload-key">
                            <Input
                                id="workload-key"
                                type="password"
                                autoComplete="new-password"
                                value={key}
                                onChange={(e) => setKey(e.target.value)}
                                placeholder={data.configured ? 'Stored key' : ''}
                            />
                        </Field>
                        <Field label="Proxmox node" id="workload-node">
                            <Select
                                items={nodes}
                                value={settings.node || null}
                                onValueChange={(value) => value && set('node', value)}
                            >
                                <SelectTrigger id="workload-node" className="w-full">
                                    <SelectValue placeholder="Choose a node" />
                                </SelectTrigger>
                                <SelectContent>
                                    {Object.keys(nodes).map((node) => (
                                        <SelectItem key={node} value={node}>
                                            {node}
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </Field>
                        <Field label="Keep host headroom (GiB)" id="workload-ram">
                            <Input
                                id="workload-ram"
                                type="number"
                                min={1}
                                max={128}
                                required
                                value={settings.reserveGiB}
                                onChange={(e) => set('reserveGiB', Number(e.target.value))}
                            />
                        </Field>
                        <Field label="Queues accepting heavy work at once" id="workload-concurrency">
                            <Input
                                id="workload-concurrency"
                                type="number"
                                min={1}
                                max={4}
                                required
                                value={settings.maxQueues}
                                onChange={(e) => set('maxQueues', Number(e.target.value))}
                            />
                        </Field>
                    </div>
                    <p className="text-xs text-muted-foreground">
                        Queue count is separate from jobs per queue. Set worker concurrency in Immich. Host headroom
                        uses Proxmox memory accounting, so it is a conservative admission check rather than a memory
                        reservation.
                    </p>
                    <fieldset className="flex flex-col gap-3">
                        <legend className="mb-3 text-sm font-medium">Queues to control</legend>
                        {Object.entries(queueNames).map(([name, label]) => (
                            <div key={name} className="flex items-center justify-between gap-3">
                                <Label htmlFor={`queue-${name}`}>{label}</Label>
                                <Switch
                                    id={`queue-${name}`}
                                    checked={settings.queues.includes(name)}
                                    onCheckedChange={(checked) =>
                                        set(
                                            'queues',
                                            checked
                                                ? [...settings.queues, name]
                                                : settings.queues.filter((queue) => queue !== name),
                                        )
                                    }
                                />
                            </div>
                        ))}
                    </fieldset>
                    <div className="flex items-center justify-between gap-3">
                        <Label htmlFor="workload-window">Only process during this daily window</Label>
                        <Switch
                            id="workload-window"
                            checked={settings.window}
                            onCheckedChange={(value) => set('window', value)}
                        />
                    </div>
                    <div className="grid gap-4 sm:grid-cols-3">
                        <Field label="Start" id="workload-start">
                            <Input
                                id="workload-start"
                                type="time"
                                required
                                value={settings.start}
                                onChange={(e) => set('start', e.target.value)}
                            />
                        </Field>
                        <Field label="End" id="workload-end">
                            <Input
                                id="workload-end"
                                type="time"
                                required
                                value={settings.end}
                                onChange={(e) => set('end', e.target.value)}
                            />
                        </Field>
                        <Field label="Timezone" id="workload-timezone">
                            <Input
                                id="workload-timezone"
                                required
                                value={settings.timezone}
                                onChange={(e) => set('timezone', e.target.value)}
                                placeholder="Europe/Brussels"
                            />
                        </Field>
                    </div>
                </CardContent>
                <CardFooter>
                    <Button disabled={save.isPending} type="submit">
                        Save controls
                    </Button>
                </CardFooter>
            </form>
        </Card>
    )
}

function Field({ label, id, children }: { label: string; id: string; children: React.ReactNode }) {
    return (
        <div className="flex flex-col gap-2">
            <Label htmlFor={id}>{label}</Label>
            {children}
        </div>
    )
}
