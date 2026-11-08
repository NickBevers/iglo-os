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
// Full title and season selections use folder paths, so a large title does
// not consume one request entry per episode. The worker expands ready files.
export function submissionPaths(folders: LibraryFolder[], selected: Set<string>): string[] {
    return folders.flatMap((folder) => {
        const stats = folderStats(folder, 'standard')
        if (folder.path.includes('/') && stats.ready.length > 0 && stats.ready.every((f) => selected.has(f.path)))
            return [folder.path]
        return [
            ...(folder.files ?? []).filter((f) => f.status === 'ready' && selected.has(f.path)).map((f) => f.path),
            ...submissionPaths(folder.folders ?? [], selected),
        ]
    })
}
export function shrinkInterval(jobs: ShrinkJob[] | undefined): number | false {
    return jobs?.some((j) => j.state === 'queued' || j.state === 'running' || (j.state === 'done' && !j.rescanned))
        ? 60000
        : false
}
export function gb(bytes: number) {
    return `${(bytes / 1e9).toLocaleString(undefined, { maximumFractionDigits: 2 })} GB`
}
