import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog'
import { api, type LibraryFile, type LibraryFolder, type ShrinkProfile, type ShrinkRefusal } from '@/lib/api'
import {
    folderStats,
    gb,
    kindNames,
    maxShrinkFiles,
    selectionState,
    shrinkInterval,
    submissionPaths,
    toggleFiles,
} from './shrink-tree'

type TreeProps = {
    profile: ShrinkProfile
    selected: Set<string>
    select: (files: LibraryFile[]) => void
    stats: Map<string, ReturnType<typeof folderStats>>
}
function FileCheckbox({
    name,
    files,
    selected,
    select,
}: { name: string; files: LibraryFile[] } & Pick<TreeProps, 'selected' | 'select'>) {
    const state = selectionState(files, selected)
    return (
        <input
            type="checkbox"
            aria-label={`Select ${name}`}
            checked={state.checked}
            aria-checked={state.mixed ? 'mixed' : state.checked}
            disabled={state.disabled}
            ref={(node) => {
                if (node) node.indeterminate = state.mixed
            }}
            onChange={() => select(files)}
            className="size-4 shrink-0 accent-primary"
        />
    )
}
function FolderRow({ folder, ...props }: { folder: LibraryFolder } & TreeProps) {
    const [expanded, setExpanded] = useState(false)
    const stats = props.stats.get(folder.path)!
    const name = kindNames[folder.path] ?? folder.name
    return (
        <li className="min-w-0">
            <div className="flex flex-wrap items-center gap-2 py-2">
                <FileCheckbox name={name} files={stats.ready} selected={props.selected} select={props.select} />
                <button
                    type="button"
                    aria-expanded={expanded}
                    onClick={() => setExpanded(!expanded)}
                    className="min-w-0 rounded px-1 text-left font-medium break-words focus-visible:ring-2 focus-visible:ring-ring"
                >
                    {expanded ? '▾' : '▸'} {name}
                </button>
                <span className="text-sm text-muted-foreground">
                    {gb(stats.bytes)} · about {gb(stats.after)} after
                </span>
            </div>
            {expanded && (
                <ul className="ml-4 border-l pl-3">
                    {(folder.folders ?? []).map((child) => (
                        <FolderRow key={child.path} folder={child} {...props} />
                    ))}
                    {(folder.files ?? []).map((file) => (
                        <li key={file.path} className="flex flex-wrap items-center gap-2 py-2 text-sm">
                            <FileCheckbox
                                name={file.path}
                                files={file.status === 'ready' ? [file] : []}
                                selected={props.selected}
                                select={props.select}
                            />
                            <span className="min-w-0 break-all">{file.path.split('/').at(-1)}</span>
                            <span className="text-muted-foreground">
                                {gb(file.bytes)} · about{' '}
                                {gb(file.status === 'ready' ? file.estimates[props.profile] : file.bytes)} after ·{' '}
                                {file.status === 'small' ? 'already small' : file.status}
                                {file.reason && `: ${file.reason}`}
                            </span>
                        </li>
                    ))}
                </ul>
            )}
        </li>
    )
}

export default function ShrinkLibrarySection() {
    const client = useQueryClient()
    const [scanBaseline, setScanBaseline] = useState<string | null>(null)
    const library = useQuery({
        queryKey: ['shrink-library'],
        queryFn: api.library,
        refetchInterval: (q) =>
            q.state.data?.scanning || (scanBaseline !== null && (q.state.data?.scannedAt ?? '') === scanBaseline)
                ? 60000
                : false,
    })
    const queue = useQuery({
        queryKey: ['shrink-jobs'],
        queryFn: api.shrinkJobs,
        refetchInterval: (q) => shrinkInterval(q.state.data?.jobs),
    })
    const terminalChanges = queue.data?.jobs
        .filter((job) => !['queued', 'running'].includes(job.state))
        .map((job) => `${job.id}:${job.state}`)
        .join(',')
    useEffect(() => {
        if (terminalChanges) void client.invalidateQueries({ queryKey: ['shrink-library'] })
    }, [client, terminalChanges])
    const scanning =
        library.data?.scanning || (scanBaseline !== null && (library.data?.scannedAt ?? '') === scanBaseline)
    const [profile, setProfile] = useState<ShrinkProfile>('standard')
    const [selected, setSelected] = useState(new Set<string>())
    const [confirmation, setConfirmation] = useState<{
        paths: string[]
        profile: ShrinkProfile
        summary: string
    } | null>(null)
    const [understood, setUnderstood] = useState(false)
    const [refused, setRefused] = useState<ShrinkRefusal[]>([])
    const trigger = useRef<HTMLButtonElement>(null)
    const refresh = () => {
        void client.invalidateQueries({ queryKey: ['shrink-library'] })
        void client.invalidateQueries({ queryKey: ['shrink-jobs'] })
    }
    const change = useMutation({
        mutationFn: async (action: () => Promise<unknown>) => action(),
        onSuccess: refresh,
        onError: (e) => toast.error(e.message),
    })
    const stats = useMemo(() => {
        const index = new Map<string, ReturnType<typeof folderStats>>()
        const visit = (folder: LibraryFolder) => {
            index.set(folder.path, folderStats(folder, profile))
            for (const child of folder.folders ?? []) visit(child)
        }
        for (const kind of library.data?.kinds ?? []) visit(kind)
        return index
    }, [library.data, profile])
    const files = (library.data?.kinds ?? [])
        .flatMap((kind) => stats.get(kind.path)?.ready ?? [])
        .filter((f) => selected.has(f.path))
    const summary = `${files.length} files selected, ${gb(files.reduce((n, f) => n + f.bytes, 0))} now, about ${gb(files.reduce((n, f) => n + f.estimates[profile], 0))} after`
    const close = () => {
        setConfirmation(null)
        setUnderstood(false)
    }
    const submit = () => {
        if (!confirmation || !understood || confirmation.paths.length > maxShrinkFiles) return
        const input = confirmation
        change.mutate(async () => {
            const result = await api.submitShrink({
                paths: input.paths,
                profile: input.profile,
                confirm: 'replace-originals',
            })
            if (result.jobs.length > 0) {
                try {
                    await api.shrinkSettings(false)
                } catch (error) {
                    toast.error(
                        `Jobs were added, but the queue could not resume: ${error instanceof Error ? error.message : 'Try Resume shrink queue.'}`,
                    )
                }
            }
            setRefused(result.refused)
            setSelected(new Set())
            close()
        })
    }
    return (
        <section aria-labelledby="shrink-heading" className="flex flex-col gap-4">
            <h2 id="shrink-heading" className="text-xl font-semibold">
                Shrink library
            </h2>
            <p className="text-sm text-muted-foreground">
                The GPU re-encodes to HEVC. Anime keeps only Japanese audio when available. Subtitles and fonts stay.
            </p>
            {(library.isPending || queue.isPending) && <p>Loading the library…</p>}
            {(library.error || queue.error) && <p role="alert">{library.error?.message || queue.error?.message}</p>}
            <Card>
                <CardContent className="flex flex-col gap-4 pt-6">
                    <div className="flex flex-wrap items-center gap-3">
                        <label htmlFor="shrink-profile">Profile</label>
                        <select
                            id="shrink-profile"
                            value={profile}
                            onChange={(e) => setProfile(e.target.value as ShrinkProfile)}
                            className="rounded-md border bg-background p-2 text-sm"
                        >
                            <option value="standard">Standard (about 0.27 GB per 24-minute episode)</option>
                            <option value="high">High (about 0.37 GB per 24-minute episode)</option>
                        </select>
                        <Button
                            variant="outline"
                            disabled={change.isPending || scanning}
                            onClick={() =>
                                change.mutate(async () => {
                                    setScanBaseline(library.data?.scannedAt ?? '')
                                    try {
                                        await api.scanLibrary()
                                    } catch (error) {
                                        setScanBaseline(null)
                                        throw error
                                    }
                                })
                            }
                        >
                            {scanning ? 'Scanning…' : 'Scan library'}
                        </Button>
                    </div>
                    <p className="text-sm text-muted-foreground">
                        Last scanned:{' '}
                        {library.data?.scannedAt
                            ? new Date(library.data.scannedAt).toLocaleString()
                            : 'Not yet scanned'}
                    </p>
                    {library.data?.error && <p role="alert">{library.data.error}</p>}
                    <ul aria-label="Library folders" className="divide-y">
                        {(library.data?.kinds ?? []).map((folder) => (
                            <FolderRow
                                key={folder.path}
                                folder={folder}
                                profile={profile}
                                selected={selected}
                                select={(files) => setSelected((current) => toggleFiles(files, current))}
                                stats={stats}
                            />
                        ))}
                    </ul>
                    <p aria-live="polite" className="text-sm">
                        {summary}
                    </p>
                    <p className="text-sm text-muted-foreground" aria-live="polite">
                        {files.length > maxShrinkFiles
                            ? `Select fewer files. You can shrink up to ${maxShrinkFiles} files at a time.`
                            : `Up to ${maxShrinkFiles} files per submission. The queue must have room for them.`}
                    </p>
                    <Button
                        ref={trigger}
                        className="self-start"
                        disabled={files.length === 0 || files.length > maxShrinkFiles || change.isPending || scanning}
                        onClick={() => {
                            setUnderstood(false)
                            setConfirmation({
                                paths: submissionPaths(library.data?.kinds ?? [], new Set(files.map((f) => f.path))),
                                profile,
                                summary,
                            })
                        }}
                    >
                        Shrink…
                    </Button>
                </CardContent>
            </Card>
            {refused.length > 0 && (
                <div role="status">
                    <h3 className="font-semibold">Refused titles</h3>
                    <ul>
                        {refused.map((item) => (
                            <li key={item.path} className="text-sm break-words">
                                {item.title}: {item.reason}
                            </li>
                        ))}
                    </ul>
                </div>
            )}
            {queue.data && (
                <Card>
                    <CardHeader>
                        <CardTitle>Shrink jobs</CardTitle>
                    </CardHeader>
                    <CardContent className="flex flex-col gap-4">
                        <Button
                            variant="outline"
                            className="self-start"
                            disabled={change.isPending}
                            onClick={() => change.mutate(() => api.shrinkSettings(!queue.data!.paused))}
                        >
                            {queue.data.paused ? 'Resume shrink queue' : 'Pause shrink queue'}
                        </Button>
                        {queue.data.jobs.length === 0 && (
                            <p className="text-sm text-muted-foreground">No shrink jobs yet.</p>
                        )}
                        {[...queue.data.jobs].reverse().map((job) => (
                            <div key={job.id} className="flex flex-col gap-2 border-t pt-3 text-sm">
                                <p className="break-all">
                                    {job.path} · {job.state}
                                </p>
                                {job.state === 'running' && (
                                    <progress
                                        aria-label={`Shrink progress for ${job.path}`}
                                        value={job.progress}
                                        max={1}
                                        className="h-2 w-full accent-primary"
                                    />
                                )}
                                {job.state === 'done' && <p>Saved {gb(job.savedBytes)}</p>}
                                {job.error && <p className="break-words">{job.error}</p>}
                                {job.state === 'not-smaller' && !job.error && (
                                    <p>The result was not small enough. The original stays.</p>
                                )}
                                {job.state === 'changed' && !job.error && (
                                    <p>The source changed during conversion. The original stays.</p>
                                )}
                                {['queued', 'running'].includes(job.state) && (
                                    <Button
                                        variant="outline"
                                        className="self-start"
                                        aria-label={`Cancel shrink for ${job.path}`}
                                        disabled={change.isPending}
                                        onClick={() => change.mutate(() => api.cancelShrink(job.id))}
                                    >
                                        Cancel
                                    </Button>
                                )}
                            </div>
                        ))}
                    </CardContent>
                </Card>
            )}
            <Dialog
                open={confirmation !== null}
                onOpenChange={(open) => {
                    if (!open && !change.isPending) close()
                }}
            >
                <DialogContent finalFocus={trigger}>
                    <DialogHeader>
                        <DialogTitle>Shrink selected library files?</DialogTitle>
                        <DialogDescription>
                            {confirmation?.summary}. The originals are replaced and can't be restored.
                        </DialogDescription>
                    </DialogHeader>
                    <label className="flex items-center gap-2 text-sm">
                        <input
                            type="checkbox"
                            checked={understood}
                            onChange={(e) => setUnderstood(e.target.checked)}
                            disabled={change.isPending}
                            className="size-4 accent-primary"
                        />
                        I understand the originals are replaced
                    </label>
                    <DialogFooter>
                        <Button variant="outline" disabled={change.isPending} onClick={close}>
                            Cancel
                        </Button>
                        <Button
                            disabled={
                                !understood || change.isPending || (confirmation?.paths.length ?? 0) > maxShrinkFiles
                            }
                            onClick={submit}
                        >
                            {change.isPending ? 'Submitting…' : 'Replace originals and shrink'}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </section>
    )
}
