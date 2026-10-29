import { useRef, useState, type SubmitEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
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
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { api, type Apps, type CatalogApp, type MediaStackAnswers, type SavedAnswers } from '@/lib/api'
import { appsQuery } from '@/lib/queries'
import { Field, Section } from './form-parts'
import { JellyfinForm } from './jellyfin-form'
import { ScopedForm } from './scoped-form'
import { HeadscaleForm } from './headscale-form'
import { MinecraftForm } from './minecraft-form'
import { keepSaved } from './keep-saved'
import { VpnFields } from './vpn-fields'
import { useVideosShares } from './use-videos-shares'
import { VideosShareFields } from './videos-share'
import { FormError } from './form-error'

type InstallDialogProps = {
    app: CatalogApp
    defaults: Apps['defaults']
    // saved are the answers of the last failed install. Their secrets stay on the host.
    saved: SavedAnswers | null
    open: boolean
    onOpenChange: (open: boolean) => void
}

const descriptions: Record<string, string> = {
    headscale:
        'Install your own Headscale controller and HTTPS proxy. Public DNS and router settings stay under your control.',
    photos: 'Install Immich in an isolated container with a local database and scoped access to your Photos and Backups shares.',
    music: 'Install a music worker with local staging and write access to your Music share.',
    inbox: 'Install the native FFmpeg worker with read access to Inbox and write access to the Videos output share.',
    gaming: 'Experimental. Install Wolf with ES-DE in a privileged container, with read access to your Games share. It only runs while you play.',
    jellyfin: 'Iglo-OS makes a new container for Jellyfin on this host, with read access to your movies and series.',
    media: 'Iglo-OS makes a new container for it on this host. Nothing else changes, except that Jellyfin gets read access to the Videos share, if it has none yet.',
    minecraft: 'Iglo-OS makes a new container on this host with a Paper server: the Minecraft Java server, but faster.',
}

export function InstallDialog({ app, defaults, saved, open, onOpenChange }: InstallDialogProps) {
    // A form that you typed in asks before Esc or a click outside closes it,
    // because closing drops what you typed. Cancel and Install close at once.
    const typed = useRef(false)

    return (
        <Dialog
            open={open}
            onOpenChange={(next, details) => {
                const stray = details.reason === 'escape-key' || details.reason === 'outside-press'
                if (!next && typed.current && stray && !window.confirm('Close this form? What you typed is lost.')) {
                    details.cancel()
                    return
                }
                onOpenChange(next)
            }}
            // Every close starts the next form clean, also one after Install.
            onOpenChangeComplete={(isOpen) => {
                if (!isOpen) {
                    typed.current = false
                }
            }}
        >
            <DialogContent
                className="max-h-[90svh] overflow-y-auto sm:max-w-xl"
                onInput={() => {
                    typed.current = true
                }}
            >
                <DialogHeader>
                    <DialogTitle>Install {app.id === 'media' ? `the ${app.name.toLowerCase()}` : app.name}</DialogTitle>
                    <DialogDescription>{descriptions[app.id]}</DialogDescription>
                </DialogHeader>
                {open &&
                    (app.id === 'headscale' ? (
                        <HeadscaleForm defaults={defaults} saved={saved} onDone={() => onOpenChange(false)} />
                    ) : app.id === 'jellyfin' ? (
                        <JellyfinForm defaults={defaults} saved={saved} onDone={() => onOpenChange(false)} />
                    ) : app.id === 'minecraft' ? (
                        <MinecraftForm defaults={defaults} saved={saved} onDone={() => onOpenChange(false)} />
                    ) : ['photos', 'music', 'inbox', 'gaming'].includes(app.id) ? (
                        <ScopedForm id={app.id} defaults={defaults} saved={saved} onDone={() => onOpenChange(false)} />
                    ) : app.id === 'media' ? (
                        <MediaStackForm
                            app={app}
                            defaults={defaults}
                            saved={saved}
                            onDone={() => onOpenChange(false)}
                        />
                    ) : (
                        <p>This application has no installer.</p>
                    ))}
            </DialogContent>
        </Dialog>
    )
}

function MediaStackForm({
    app,
    defaults,
    saved,
    onDone,
}: {
    app: CatalogApp
    defaults: Apps['defaults']
    saved: SavedAnswers | null
    onDone: () => void
}) {
    const queryClient = useQueryClient()
    const shares = useVideosShares(defaults, true)
    const [answers, setAnswers] = useState<MediaStackAnswers>(
        () =>
            saved?.answers ?? {
                videosBinding: '',
                moviesFolder: defaults.moviesFolder,
                seriesFolder: defaults.seriesFolder,
                animeFolder: defaults.animeFolder,
                animeMoviesFolder: defaults.animeMoviesFolder,
                vpn: {
                    provider: defaults.vpnProvider,
                    locations: defaults.vpnLocations,
                    wireguardPrivateKey: '',
                    piaUsername: '',
                    piaPassword: '',
                    piaDedicatedToken: '',
                },
                subtitleLanguages: defaults.subtitleLanguages,
                username: defaults.username,
                password: '',
                jellyfinApiKey: '',
                jellyfinAdminUsername: '',
                jellyfinAdminPassword: '',
                openSubtitlesUsername: '',
                openSubtitlesPassword: '',
                restartJellyfin: true,
                flareSolverr: true,
                animeIndexers: true,
                storage: defaults.storage,
                downloadsSize: defaults.downloadsSize,
            },
    )

    // The share that Jellyfin uses, until you choose another one.
    const videosBinding = answers.videosBinding || shares.preferred
    const install = useMutation({
        mutationFn: () => api.installApp(app.id, { ...answers, videosBinding }, saved !== null),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: appsQuery.queryKey })
            toast.success('The install started')
            onDone()
        },
    })

    function set<K extends keyof MediaStackAnswers>(key: K, value: MediaStackAnswers[K]) {
        setAnswers((current) => ({ ...current, [key]: value }))
    }

    function handleSubmit(event: SubmitEvent) {
        event.preventDefault()
        install.mutate()
    }

    const storages = Object.fromEntries(defaults.storages.map((name) => [name, name]))
    const shortPassword = answers.password.length > 0 && answers.password.length < 12

    return (
        <form className="flex flex-col gap-6" onSubmit={handleSubmit}>
            <VideosShareFields
                id="media"
                shares={shares}
                value={videosBinding}
                onChange={(value) => set('videosBinding', value)}
                write
                help="Where your movies, series and anime are. Each gets its own folder on the share, and its own library in Jellyfin. New files get names that Jellyfin reads."
            >
                {(path) => (
                    <div className="grid gap-4 sm:grid-cols-2">
                        <Field id="movies-folder" label="Movies folder" help={`${path}/${answers.moviesFolder}`}>
                            <Input
                                id="movies-folder"
                                value={answers.moviesFolder}
                                onChange={(event) => set('moviesFolder', event.target.value)}
                                required
                            />
                        </Field>
                        <Field id="series-folder" label="Series folder" help={`${path}/${answers.seriesFolder}`}>
                            <Input
                                id="series-folder"
                                value={answers.seriesFolder}
                                onChange={(event) => set('seriesFolder', event.target.value)}
                                required
                            />
                        </Field>
                        <Field
                            id="anime-folder"
                            label="Anime folder"
                            help={
                                answers.animeFolder
                                    ? `${path}/${answers.animeFolder}`
                                    : 'Empty: anime goes in the series folder.'
                            }
                        >
                            <Input
                                id="anime-folder"
                                value={answers.animeFolder}
                                onChange={(event) => set('animeFolder', event.target.value)}
                            />
                        </Field>
                        <Field
                            id="anime-movies-folder"
                            label="Anime movies folder"
                            help={
                                answers.animeMoviesFolder
                                    ? `${path}/${answers.animeMoviesFolder}`
                                    : 'Empty: anime movies go in the movies folder.'
                            }
                        >
                            <Input
                                id="anime-movies-folder"
                                value={answers.animeMoviesFolder}
                                onChange={(event) => set('animeMoviesFolder', event.target.value)}
                            />
                        </Field>
                    </div>
                )}
            </VideosShareFields>

            <Section title="VPN" help="qBittorrent, Prowlarr and FlareSolverr only use the internet through the VPN.">
                <VpnFields
                    id="media-vpn"
                    value={answers.vpn}
                    onChange={(patch) => set('vpn', { ...answers.vpn, ...patch })}
                />
                <label className="flex items-center justify-between gap-4 text-sm">
                    <span>
                        FlareSolverr
                        <span className="block text-xs text-muted-foreground">
                            For indexers behind Cloudflare. It runs a browser of a few hundred MB, so leave it off when
                            your indexers don't need it.
                        </span>
                    </span>
                    <Switch
                        checked={answers.flareSolverr}
                        onCheckedChange={(checked) => set('flareSolverr', checked)}
                    />
                </label>
                <label className="flex items-center justify-between gap-4 text-sm">
                    <span>
                        Anime trackers
                        <span className="block text-xs text-muted-foreground">
                            Adds Nyaa.si and Anime Tosho to Prowlarr. Anime always gets a quality profile in Sonarr and
                            Radarr that rejects dubbed-only releases, and one download per episode.
                        </span>
                    </span>
                    <Switch
                        checked={answers.animeIndexers}
                        onCheckedChange={(checked) => set('animeIndexers', checked)}
                    />
                </label>
            </Section>

            <Section title="Login" help="One login for Radarr, Sonarr, Prowlarr, Bazarr and qBittorrent.">
                <Field id="arr-username" label="Username">
                    <Input
                        id="arr-username"
                        autoComplete="off"
                        value={answers.username}
                        onChange={(event) => set('username', event.target.value.trim())}
                        required
                    />
                </Field>
                <Field
                    id="arr-password"
                    label="Password"
                    help={shortPassword ? undefined : 'At least 12 characters.'}
                    error={shortPassword ? 'The password needs at least 12 characters.' : undefined}
                >
                    <Input
                        id="arr-password"
                        type="password"
                        autoComplete="new-password"
                        value={answers.password}
                        onChange={(event) => set('password', event.target.value)}
                        aria-invalid={shortPassword}
                        placeholder={saved ? keepSaved : undefined}
                        required={!saved}
                    />
                </Field>
            </Section>

            {defaults.jellyfinVmid ? (
                <Section
                    title="Jellyfin"
                    help={`Found in container ${defaults.jellyfinVmid}. It gets read access to the Videos share.`}
                >
                    <Field
                        id="jellyfin-key"
                        label="API key (optional)"
                        help="With a key, the install adds the Movies and Series libraries. Make one in Jellyfin: Dashboard > API Keys."
                    >
                        <Input
                            id="jellyfin-key"
                            type="password"
                            autoComplete="off"
                            value={answers.jellyfinApiKey}
                            onChange={(event) => set('jellyfinApiKey', event.target.value.trim())}
                            placeholder={saved?.hasJellyfinApiKey ? keepSaved : undefined}
                        />
                    </Field>
                    <div className="grid gap-4 sm:grid-cols-2">
                        <Field
                            id="jellyfin-admin"
                            label="Admin username (optional)"
                            help="With a Jellyfin admin, Iglo-OS also sets up Seerr. You log in to Seerr with this account."
                        >
                            <Input
                                id="jellyfin-admin"
                                autoComplete="off"
                                value={answers.jellyfinAdminUsername}
                                onChange={(event) => set('jellyfinAdminUsername', event.target.value.trim())}
                            />
                        </Field>
                        <Field id="jellyfin-admin-password" label="Admin password">
                            <Input
                                id="jellyfin-admin-password"
                                type="password"
                                autoComplete="off"
                                value={answers.jellyfinAdminPassword}
                                onChange={(event) => set('jellyfinAdminPassword', event.target.value)}
                                placeholder={
                                    saved?.hasJellyfinAdminPassword &&
                                    answers.jellyfinAdminUsername === saved.answers.jellyfinAdminUsername
                                        ? keepSaved
                                        : undefined
                                }
                                disabled={!answers.jellyfinAdminUsername}
                            />
                        </Field>
                    </div>
                    <label className="flex items-center justify-between gap-4 text-sm">
                        <span>
                            Restart Jellyfin
                            <span className="block text-xs text-muted-foreground">
                                Jellyfin only sees the Videos share after a restart.
                            </span>
                        </span>
                        <Switch
                            checked={answers.restartJellyfin}
                            onCheckedChange={(checked) => set('restartJellyfin', checked)}
                        />
                    </label>
                </Section>
            ) : null}

            <Section title="Container">
                <div className="grid gap-4 sm:grid-cols-2">
                    <Field id="storage" label="Storage">
                        <Select
                            items={storages}
                            value={answers.storage}
                            onValueChange={(value) => value && set('storage', value)}
                        >
                            <SelectTrigger id="storage" className="w-full">
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
                    <Field id="downloads-size" label="Downloads disk (GB)">
                        <Input
                            id="downloads-size"
                            type="number"
                            min={10}
                            max={10000}
                            value={answers.downloadsSize}
                            onChange={(event) => set('downloadsSize', Number(event.target.value))}
                            required
                        />
                    </Field>
                </div>
            </Section>

            <Section
                title="Subtitles"
                help="Bazarr adds subtitles to every new movie and episode, except anime, which keeps the official subtitles in the file."
            >
                <Field id="subtitles" label="Languages" help="2-letter codes, like en,nl.">
                    <Input
                        id="subtitles"
                        value={answers.subtitleLanguages}
                        onChange={(event) => set('subtitleLanguages', event.target.value.trim())}
                        required
                    />
                </Field>
                <div className="grid gap-4 sm:grid-cols-2">
                    <Field
                        id="opensubtitles-username"
                        label="OpenSubtitles.com username (optional)"
                        help={
                            <>
                                Finds many more subtitles. Make a free account at{' '}
                                <a
                                    className="underline"
                                    href="https://www.opensubtitles.com"
                                    target="_blank"
                                    rel="noreferrer"
                                >
                                    opensubtitles.com
                                </a>
                                , not .org.
                            </>
                        }
                    >
                        <Input
                            id="opensubtitles-username"
                            autoComplete="off"
                            value={answers.openSubtitlesUsername}
                            onChange={(event) => set('openSubtitlesUsername', event.target.value.trim())}
                        />
                    </Field>
                    <Field id="opensubtitles-password" label="OpenSubtitles.com password">
                        <Input
                            id="opensubtitles-password"
                            type="password"
                            autoComplete="off"
                            value={answers.openSubtitlesPassword}
                            onChange={(event) => set('openSubtitlesPassword', event.target.value)}
                            placeholder={
                                saved?.hasOpenSubtitlesPassword &&
                                answers.openSubtitlesUsername === saved.answers.openSubtitlesUsername
                                    ? keepSaved
                                    : undefined
                            }
                            disabled={!answers.openSubtitlesUsername}
                        />
                    </Field>
                </div>
            </Section>

            <FormError error={install.error} title="The install did not start" />
            <DialogFooter>
                <Button type="button" variant="outline" onClick={onDone}>
                    Cancel
                </Button>
                <Button type="submit" disabled={install.isPending || shortPassword || (!saved && !answers.password)}>
                    {install.isPending && <Loader2Icon className="animate-spin" />}
                    Install
                </Button>
            </DialogFooter>
        </form>
    )
}
