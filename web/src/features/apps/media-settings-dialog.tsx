import { useId, useState, type SubmitEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2Icon } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog'
import { api, subtitleLanguageButtons, toggleSubtitleLanguage, type CatalogApp, type MediaSettings } from '@/lib/api'
import { appsQuery, mediaSettingsQuery } from '@/lib/queries'

type MediaKind = 'anime' | 'dutch' | 'series' | 'films'

const kinds: { key: MediaKind; name: string; audio: string; help: string }[] = [
    {
        key: 'anime',
        name: 'Anime',
        audio: 'Japanese, the original.',
        help: 'The releases bring the official English subtitles. Bazarr leaves anime alone unless you pick a language.',
    },
    {
        key: 'dutch',
        name: 'Dutch',
        audio: 'Dutch, dubs too. Dutch originals get this by themselves. For a dub like De Smurfen, pick the Dutch (Iglo-OS) profile, or Dutch 4K (Iglo-OS) for 4K films, and the dutch tag when you request it in Seerr.',
        help: 'None keeps Dutch series and films without subtitles.',
    },
    {
        key: 'series',
        name: 'Series',
        audio: 'The original language.',
        help: 'Bazarr downloads them for every title of this kind. Your player decides when to show them.',
    },
    {
        key: 'films',
        name: 'Films',
        audio: 'The original language.',
        help: 'Bazarr downloads them for every title of this kind. Your player decides when to show them.',
    },
]

type MediaSettingsDialogProps = { app: CatalogApp; open: boolean; onOpenChange: (open: boolean) => void }

export function MediaSettingsDialog({ app, open, onOpenChange }: MediaSettingsDialogProps) {
    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="flex max-h-[calc(100dvh-2rem)] flex-col sm:max-w-2xl">
                <DialogHeader>
                    <DialogTitle>Languages</DialogTitle>
                    <DialogDescription>
                        Choose subtitle languages, film quality and maximum download sizes. Audio stays fixed.
                    </DialogDescription>
                </DialogHeader>
                {open && <MediaSettingsLoader app={app} onDone={() => onOpenChange(false)} />}
            </DialogContent>
        </Dialog>
    )
}

function MediaSettingsLoader({ app, onDone }: { app: CatalogApp; onDone: () => void }) {
    const { data, error, isFetching } = useQuery(mediaSettingsQuery(app.id))

    if (error) {
        return (
            <div role="alert" className="flex flex-col gap-2 text-sm">
                <p className="text-destructive">{error.message}</p>
            </div>
        )
    }

    if (!data || isFetching) {
        return (
            <p role="status" className="flex items-center gap-2 py-4 text-sm text-muted-foreground">
                <Loader2Icon aria-hidden="true" className="size-4 animate-spin" />
                Loading languages…
            </p>
        )
    }

    return <MediaSettingsForm app={app} current={data.settings} onDone={onDone} />
}

function MediaSettingsForm({ app, current, onDone }: { app: CatalogApp; current: MediaSettings; onDone: () => void }) {
    const queryClient = useQueryClient()
    const id = useId()
    const [settings, setSettings] = useState(current)
    const [openedWith] = useState(current)
    const sizes = {
        series: settings.sizes?.series ?? 0,
        films: settings.sizes?.films ?? 0,
        films4k: settings.sizes?.films4k ?? 0,
    }
    const changed =
        (settings.uhdFilms ?? false) !== (current.uhdFilms ?? false) ||
        kinds.some(({ key }) => settings[key].subtitles.join(',') !== current[key].subtitles.join(',')) ||
        sizes.series !== (current.sizes?.series ?? 0) ||
        sizes.films !== (current.sizes?.films ?? 0) ||
        sizes.films4k !== (current.sizes?.films4k ?? 0)
    const change = useMutation({
        mutationFn: () => api.changeMediaSettings(app.id, settings),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: appsQuery.queryKey, exact: true })
            queryClient.invalidateQueries({ queryKey: mediaSettingsQuery(app.id).queryKey })
            onDone()
        },
        onError: (error) => toast.error('Could not change media settings', { description: error.message }),
    })

    const submit = (event: SubmitEvent) => {
        event.preventDefault()
        if (changed && !change.isPending) change.mutate()
    }

    return (
        <form onSubmit={submit} className="flex min-h-0 flex-col gap-4">
            <div className="flex min-h-0 flex-col gap-5 overflow-y-auto p-1">
                {kinds.map(({ key, name, audio, help }) => {
                    const selected = settings[key].subtitles
                    const allowEmpty = key === 'anime' || key === 'dutch'
                    const languages = subtitleLanguageButtons(openedWith[key].subtitles)
                    const helpId = `${id}-${key}-help`
                    const limitId = `${id}-${key}-limit`
                    return (
                        <section key={key} aria-labelledby={`${id}-${key}`} className="flex flex-col gap-2">
                            <h3 id={`${id}-${key}`} className="font-semibold">
                                {name}
                            </h3>
                            <p className="text-sm text-muted-foreground">
                                <span className="font-medium text-foreground">Audio: </span>
                                {audio}
                            </p>
                            <fieldset
                                disabled={change.isPending}
                                aria-describedby={`${helpId} ${limitId}`}
                                className="flex flex-col gap-2"
                            >
                                <legend className="mb-2 text-sm font-medium">
                                    Subtitles<span className="sr-only"> for {name}</span>
                                </legend>
                                <p id={helpId} className="text-xs text-muted-foreground">
                                    {help}
                                </p>
                                <p id={limitId} className="text-xs text-muted-foreground">
                                    Pick up to 10. {!allowEmpty && 'Series and films need at least one language.'}
                                </p>
                                <div className="flex flex-wrap gap-1.5">
                                    {[...(allowEmpty ? [null] : []), ...languages.map(({ code }) => code)].map(
                                        (code) => {
                                            const pressed =
                                                code === null ? selected.length === 0 : selected.includes(code)
                                            const label =
                                                code === null
                                                    ? 'None'
                                                    : (languages.find((language) => language.code === code)?.name ??
                                                      code)
                                            return (
                                                <Button
                                                    key={code ?? 'none'}
                                                    type="button"
                                                    size="sm"
                                                    variant={pressed ? 'secondary' : 'outline'}
                                                    aria-pressed={pressed}
                                                    disabled={
                                                        change.isPending ||
                                                        (code !== null && !pressed && selected.length >= 10)
                                                    }
                                                    onClick={() =>
                                                        setSettings((now) => ({
                                                            ...now,
                                                            [key]: {
                                                                ...now[key],
                                                                subtitles: toggleSubtitleLanguage(
                                                                    now[key].subtitles,
                                                                    code,
                                                                    allowEmpty,
                                                                ),
                                                            },
                                                        }))
                                                    }
                                                >
                                                    {label}
                                                </Button>
                                            )
                                        },
                                    )}
                                </div>
                            </fieldset>
                        </section>
                    )
                })}
                <section aria-labelledby={`${id}-sizes`} className="flex flex-col gap-3">
                    <h3 id={`${id}-sizes`} className="font-semibold">
                        Maximum download size
                    </h3>
                    <p id={`${id}-sizes-help`} className="text-xs text-muted-foreground">
                        Releases above the limit are skipped. If only larger releases exist, nothing is downloaded until
                        a smaller one appears. Files you already have are not touched.
                    </p>
                    <div className="flex flex-col gap-2">
                        <label className="flex items-center justify-between gap-4 text-sm font-medium">
                            <span>Films in 4K when available</span>
                            <Switch
                                checked={settings.uhdFilms ?? false}
                                disabled={change.isPending}
                                aria-describedby={`${id}-films-4k-help`}
                                onCheckedChange={(checked) => setSettings((now) => ({ ...now, uhdFilms: checked }))}
                            />
                        </label>
                        <p id={`${id}-films-4k-help`} className="text-xs text-muted-foreground">
                            4K first, within the 4K size limit. A larger 4K release is skipped and 1080p is used
                            instead, with an upgrade once a 4K release fits. Dolby Vision releases without an HDR
                            fallback are skipped because the Pi 4 cannot play them.
                        </p>
                    </div>
                    {(
                        [
                            {
                                key: 'series',
                                label: 'Series and anime, per 24-minute episode',
                                minutes: 24,
                                gb: [3, 2, 1.5, 1],
                            },
                            {
                                key: 'films',
                                label: 'Films in HD (1080p and lower), per 2-hour film',
                                minutes: 120,
                                gb: [12, 8, 6, 4],
                            },
                            {
                                key: 'films4k',
                                label: 'Films in 4K, per 2-hour film',
                                minutes: 120,
                                gb: [40, 25, 20, 15],
                            },
                        ] as const
                    ).map(({ key, label, minutes, gb }) => {
                        // Decimal GB, rounded down to integer MB per minute.
                        const options = [
                            { value: 0, label: 'No limit' },
                            ...gb.map((size) => ({ value: Math.floor((size * 1000) / minutes), label: `${size} GB` })),
                        ]
                        const saved = openedWith.sizes?.[key] ?? 0
                        if (!options.some(({ value }) => value === saved)) {
                            options.push({ value: saved, label: `Custom (${saved} MB per minute)` })
                        }
                        return (
                            <div key={key} className="flex flex-col gap-2">
                                <label htmlFor={`${id}-size-${key}`} className="text-sm font-medium">
                                    {label}
                                </label>
                                <select
                                    id={`${id}-size-${key}`}
                                    value={sizes[key]}
                                    disabled={change.isPending}
                                    aria-describedby={
                                        key === 'films4k' ? `${id}-sizes-help ${id}-sizes-4k-help` : `${id}-sizes-help`
                                    }
                                    className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                                    onChange={(event) =>
                                        setSettings((now) => ({
                                            ...now,
                                            sizes: {
                                                series: now.sizes?.series ?? 0,
                                                films: now.sizes?.films ?? 0,
                                                films4k: now.sizes?.films4k ?? 0,
                                                [key]: Number(event.target.value),
                                            },
                                        }))
                                    }
                                >
                                    {options.map(({ value, label }) => (
                                        <option key={value} value={value}>
                                            {label}
                                        </option>
                                    ))}
                                </select>
                                {key === 'films4k' && (
                                    <p id={`${id}-sizes-4k-help`} className="text-xs text-muted-foreground">
                                        4K films are only downloaded when the media settings allow 4K.
                                    </p>
                                )}
                            </div>
                        )
                    })}
                </section>
            </div>
            <DialogFooter>
                <Button type="submit" disabled={!changed || change.isPending}>
                    {change.isPending && <Loader2Icon aria-hidden="true" className="animate-spin" />}
                    Save
                </Button>
            </DialogFooter>
        </form>
    )
}
