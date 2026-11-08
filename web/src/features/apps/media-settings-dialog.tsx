import { useId, useState, type SubmitEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2Icon } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
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

const kinds: { key: keyof MediaSettings; name: string; audio: string; help: string }[] = [
    {
        key: 'anime',
        name: 'Anime',
        audio: 'Japanese, the original.',
        help: 'The releases bring the official English subtitles. Bazarr leaves anime alone unless you pick a language.',
    },
    {
        key: 'dutch',
        name: 'Dutch',
        audio: 'Dutch, dubs too. Dutch originals get this by themselves. For a dub like De Smurfen, pick the Dutch (Iglo-OS) profile and the dutch tag when you request it in Seerr.',
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
                    <DialogDescription>Choose subtitle languages for each kind. Audio stays fixed.</DialogDescription>
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
    const changed = kinds.some(({ key }) => settings[key].subtitles.join(',') !== current[key].subtitles.join(','))
    const change = useMutation({
        mutationFn: () => api.changeMediaSettings(app.id, settings),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: appsQuery.queryKey, exact: true })
            queryClient.invalidateQueries({ queryKey: mediaSettingsQuery(app.id).queryKey })
            onDone()
        },
        onError: (error) => toast.error('Could not change languages', { description: error.message }),
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
