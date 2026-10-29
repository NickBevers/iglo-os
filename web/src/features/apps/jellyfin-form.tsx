import { useState, type SubmitEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2Icon } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { DialogFooter } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { api, type Apps, type JellyfinAnswers, type SavedAnswers } from '@/lib/api'
import { appsQuery } from '@/lib/queries'
import { Field, Section } from './form-parts'
import { keepSaved } from './keep-saved'
import { useVideosShares } from './use-videos-shares'
import { VideosShareFields } from './videos-share'
import { FormError } from './form-error'

type JellyfinFormProps = {
    defaults: Apps['defaults']
    saved: SavedAnswers | null
    onDone: () => void
}

export function JellyfinForm({ defaults, saved, onDone }: JellyfinFormProps) {
    const queryClient = useQueryClient()
    const shares = useVideosShares(defaults, false)
    const [answers, setAnswers] = useState<JellyfinAnswers>(
        () =>
            saved?.jellyfin ?? {
                videosBinding: '',
                moviesFolder: defaults.moviesFolder,
                seriesFolder: defaults.seriesFolder,
                animeFolder: defaults.animeFolder,
                animeMoviesFolder: defaults.animeMoviesFolder,
                adminUsername: 'admin',
                adminPassword: '',
                theme: true,
                storage: defaults.storage,
            },
    )

    // The share that the media stack uses, until you choose another one.
    const videosBinding = answers.videosBinding || shares.preferred
    const install = useMutation({
        mutationFn: () => api.installJellyfin({ ...answers, videosBinding }, saved !== null),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: appsQuery.queryKey })
            toast.success('The install started')
            onDone()
        },
    })

    function set<K extends keyof JellyfinAnswers>(key: K, value: JellyfinAnswers[K]) {
        setAnswers((current) => ({ ...current, [key]: value }))
    }

    function handleSubmit(event: SubmitEvent) {
        event.preventDefault()
        install.mutate()
    }

    const storages = Object.fromEntries(defaults.storages.map((name) => [name, name]))
    const passwordSaved = saved?.hasJellyfinAdminPassword && answers.adminUsername === saved.jellyfin.adminUsername

    return (
        <form className="flex flex-col gap-6" onSubmit={handleSubmit}>
            <VideosShareFields
                id="jf"
                shares={shares}
                value={videosBinding}
                onChange={(value) => set('videosBinding', value)}
                write={false}
                help="Jellyfin reads your movies, series and anime from this share, and never writes to it."
            >
                {(path) => (
                    <div className="grid gap-4 sm:grid-cols-2">
                        <Field id="jf-movies" label="Movies folder" help={`${path}/${answers.moviesFolder}`}>
                            <Input
                                id="jf-movies"
                                value={answers.moviesFolder}
                                onChange={(event) => set('moviesFolder', event.target.value)}
                                required
                            />
                        </Field>
                        <Field id="jf-series" label="Series folder" help={`${path}/${answers.seriesFolder}`}>
                            <Input
                                id="jf-series"
                                value={answers.seriesFolder}
                                onChange={(event) => set('seriesFolder', event.target.value)}
                                required
                            />
                        </Field>
                        <Field
                            id="jf-anime-folder"
                            label="Anime folder"
                            help={
                                answers.animeFolder
                                    ? `${path}/${answers.animeFolder}`
                                    : 'Empty: anime goes in the series folder.'
                            }
                        >
                            <Input
                                id="jf-anime-folder"
                                value={answers.animeFolder}
                                onChange={(event) => set('animeFolder', event.target.value)}
                            />
                        </Field>
                        <Field
                            id="jf-anime-movies-folder"
                            label="Anime movies folder"
                            help={
                                answers.animeMoviesFolder
                                    ? `${path}/${answers.animeMoviesFolder}`
                                    : 'Empty: anime movies go in the movies folder.'
                            }
                        >
                            <Input
                                id="jf-anime-movies-folder"
                                value={answers.animeMoviesFolder}
                                onChange={(event) => set('animeMoviesFolder', event.target.value)}
                            />
                        </Field>
                    </div>
                )}
            </VideosShareFields>

            <Section title="Admin account" help="Iglo-OS finishes the setup wizard of Jellyfin with this account.">
                <div className="grid gap-4 sm:grid-cols-2">
                    <Field id="jf-admin" label="Username">
                        <Input
                            id="jf-admin"
                            autoComplete="off"
                            value={answers.adminUsername}
                            onChange={(event) => set('adminUsername', event.target.value.trim())}
                            required
                        />
                    </Field>
                    <Field id="jf-password" label="Password">
                        <Input
                            id="jf-password"
                            type="password"
                            autoComplete="new-password"
                            value={answers.adminPassword}
                            onChange={(event) => set('adminPassword', event.target.value)}
                            placeholder={passwordSaved ? keepSaved : undefined}
                            required={!passwordSaved}
                        />
                    </Field>
                </div>
            </Section>

            <Section title="Container">
                <Field id="jf-storage" label="Storage">
                    <Select
                        items={storages}
                        value={answers.storage}
                        onValueChange={(value) => value && set('storage', value)}
                    >
                        <SelectTrigger id="jf-storage" className="w-full">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            {defaults.storages.map((name) => (
                                <SelectItem key={name} value={name}>
                                    {name}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                </Field>
                <label className="flex items-center justify-between gap-4 text-sm">
                    <span>
                        Netflix theme
                        <span className="block text-xs text-muted-foreground">
                            For the web page of Jellyfin. You can change it later on the Jellyfin page of Iglo-OS.
                        </span>
                    </span>
                    <Switch checked={answers.theme} onCheckedChange={(checked) => set('theme', checked)} />
                </label>
                <p className="text-xs text-muted-foreground">
                    When the host has a GPU (Intel or AMD), Jellyfin gets it for hardware transcoding.
                </p>
            </Section>

            <FormError error={install.error} title="The install did not start" />
            <DialogFooter>
                <Button type="button" variant="outline" onClick={onDone}>
                    Cancel
                </Button>
                <Button type="submit" disabled={install.isPending}>
                    {install.isPending && <Loader2Icon className="animate-spin" />}
                    Install
                </Button>
            </DialogFooter>
        </form>
    )
}
