import type { LibraryFile, LibraryFolder, ShrinkJob, ShrinkProfile } from '../../lib/api'

export const kindNames: Record<string, string> = {
    anime: 'Anime',
    series: 'Series',
    movies: 'Films',
    'anime-movies': 'Anime films',
}
export type FolderStats = { files: LibraryFile[]; ready: LibraryFile[]; bytes: number; after: number }
export function folderStats(folder: LibraryFolder, profile: ShrinkProfile): FolderStats {
    const files = [
        ...(folder.files ?? []),
        ...(folder.folders ?? []).flatMap((child) => folderStats(child, profile).files),
    ]
    return {
        files,
        ready: files.filter((f) => f.status === 'ready'),
        bytes: files.reduce((n, f) => n + f.bytes, 0),
        after: files.reduce((n, f) => n + (f.status === 'ready' ? f.estimates[profile] : f.bytes), 0),
    }
}
export function selectionState(ready: LibraryFile[], selected: Set<string>) {
    const count = ready.filter((f) => selected.has(f.path)).length
    return {
        checked: count > 0 && count === ready.length,
        mixed: count > 0 && count < ready.length,
        disabled: ready.length === 0,
    }
}
export function toggleFiles(ready: LibraryFile[], selected: Set<string>) {
    const next = new Set(selected)
    const remove = selectionState(ready, selected).checked
    for (const file of ready) {
        if (remove) next.delete(file.path)
        else next.add(file.path)
    }
    return next
}
export const maxShrinkFiles = 100

// A folder checkbox selects the files in its snapshot. Submit those exact
// files even when a later scan discovers more children under the same title.
export function submissionPaths(folders: LibraryFolder[], selected: Set<string>): string[] {
    return [
        ...new Set(
            folders.flatMap((folder) => [
                ...(folder.files ?? [])
                    .filter((file) => file.status === 'ready' && selected.has(file.path))
                    .map((file) => file.path),
                ...submissionPaths(folder.folders ?? [], selected),
            ]),
        ),
    ]
}
export function shrinkInterval(jobs: ShrinkJob[] | undefined): number | false {
    return jobs?.some((j) => j.state === 'queued' || j.state === 'running' || (j.state === 'done' && !j.rescanned))
        ? 60000
        : false
}
export function gb(bytes: number) {
    return `${(bytes / 1e9).toLocaleString(undefined, { maximumFractionDigits: 2 })} GB`
}

// The worker lists jobs in the order they were added. Show the running job
// first, then the queue in the order it will run, then finished jobs with the
// newest first.
export function orderJobs(jobs: ShrinkJob[]): ShrinkJob[] {
    const rank = (job: ShrinkJob) => (job.state === 'running' ? 0 : job.state === 'queued' ? 1 : 2)
    const finished = (job: ShrinkJob) => (job.finishedAt ? Date.parse(job.finishedAt) : 0)
    return jobs
        .map((job, index) => ({ job, index }))
        .sort((a, b) => {
            const byRank = rank(a.job) - rank(b.job)
            if (byRank !== 0) return byRank
            if (rank(a.job) < 2) return a.index - b.index
            return finished(b.job) - finished(a.job) || b.index - a.index
        })
        .map(({ job }) => job)
}

const finishedLabels: Partial<Record<ShrinkJob['state'], string>> = {
    failed: 'Failed',
    'not-smaller': 'Not smaller, original kept',
    changed: 'Source changed, original kept',
    cancelled: 'Cancelled',
}

// jobStatus is the short status shown on the same line as the file name.
export function jobStatus(job: ShrinkJob): string {
    switch (job.state) {
        case 'running':
            return `${Math.floor(job.progress * 100)}%`
        case 'queued':
            return 'Queued'
        case 'done':
            return `Done, saved ${gb(job.savedBytes)}`
        default:
            return finishedLabels[job.state] ?? job.state
    }
}

export function jobName(path: string): string {
    return path.split('/').pop() ?? path
}
