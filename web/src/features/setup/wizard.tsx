import { useEffect, useRef, useState, type SubmitEvent, type ReactNode } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { BackupPasswordFields } from '@/features/apps/backup-password'
import { backupPasswordProblem } from '@/features/apps/backup-password-check'
import { Field } from '@/features/apps/form-parts'
import { FormError } from '@/features/apps/form-error'
import { VpnFields } from '@/features/apps/vpn-fields'
import { api, type Setup } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { cn } from '@/lib/utils'
import { connectedShare, draftFromPlan, newDraft, planOf, readOnlyRoles, rolesOf, shareNames, type Draft } from './plan'
import { forgetDraft, loadDraft, saveDraft } from './saved-draft'

const pages = ['Apps', 'NAS', 'Accounts', 'Review'] as const
type Page = (typeof pages)[number]

// Wizard asks for everything first, and starts nothing until the review.
export function Wizard({ setup }: { setup: Setup }) {
    // Answers from before a reload come back, without their secrets.
    const [restored] = useState(() => loadDraft(setup))
    const [draft, setDraft] = useState<Draft>(() => restored?.draft ?? newDraft(setup))
    const [page, setPage] = useState<Page>(() => pages.find((name) => name === restored?.page) ?? 'Apps')
    // Nothing is kept until you change something, so an untouched form
    // doesn't come back as answers.
    const [first] = useState(draft)
    useEffect(() => {
        if (restored || draft !== first || page !== 'Apps') {
            saveDraft(draft, page)
        }
    }, [draft, page, first, restored])
    // A new page takes the focus, so keyboard and screen reader users start at
    // its top instead of on the button they pressed. The first page doesn't.
    const region = useRef<HTMLElement>(null)
    const moved = useRef(false)
    useEffect(() => {
        if (moved.current) region.current?.focus()
    }, [page])
    const go = (to: Page) => {
        moved.current = true
        setPage(to)
    }
    const client = useQueryClient()
    const start = useMutation({
        mutationFn: () => api.startSetup(planOf(draft)),
        onSuccess: () => {
            forgetDraft()
            client.invalidateQueries({ queryKey: ['setup'] })
        },
    })
    const roles = rolesOf(draft, setup)
    const wants = (app: string) => draft.apps.includes(app)

    function next(event: SubmitEvent) {
        event.preventDefault()
        if (page === 'Accounts' && draft.admin.password !== draft.confirm) {
            toast.error('The two admin passwords differ')
            return
        }
        const backupProblem = backupPasswordProblem(draft.backupPassword, draft.backupConfirm)
        if (page === 'Accounts' && wants('photos') && backupProblem) {
            toast.error(`Backup password: ${backupProblem}`)
            return
        }
        if (page === 'Review') {
            start.mutate()
            return
        }
        go(pages[pages.indexOf(page) + 1])
    }

    return (
        <form className="flex flex-col gap-6" onSubmit={next}>
            {restored && (
                <Alert>
                    <AlertDescription>
                        Your answers came back after the reload. Type the passwords and keys again, and confirm the
                        privileged container if you chose Gaming.
                    </AlertDescription>
                </Alert>
            )}
            <ol className="flex flex-wrap gap-2 text-sm" aria-label="Setup steps">
                {pages.map((name, index) => (
                    <li
                        key={name}
                        aria-current={name === page ? 'step' : undefined}
                        className={cn(
                            'rounded-full border px-3 py-1 text-muted-foreground',
                            name === page && 'border-brand bg-brand-soft text-brand',
                        )}
                    >
                        {index + 1}. {name}
                    </li>
                ))}
            </ol>

            <section
                ref={region}
                tabIndex={-1}
                aria-label={`Step ${pages.indexOf(page) + 1} of ${pages.length}: ${page}`}
                className="outline-none"
            >
                {page === 'Apps' && <AppsPage setup={setup} draft={draft} setDraft={setDraft} />}
                {page === 'NAS' && <NASPage setup={setup} draft={draft} setDraft={setDraft} roles={roles} />}
                {page === 'Accounts' && <AccountsPage draft={draft} setDraft={setDraft} wants={wants} />}
                {page === 'Review' && <ReviewPage setup={setup} draft={draft} roles={roles} />}
            </section>

            {page === 'Review' && <FormError error={start.error} title="The setup did not start" />}
            <div className="flex gap-2">
                {page !== 'Apps' && (
                    <Button type="button" variant="outline" onClick={() => go(pages[pages.indexOf(page) - 1])}>
                        Back
                    </Button>
                )}
                <Button type="submit" disabled={start.isPending || (page === 'Apps' && draft.apps.length === 0)}>
                    {page === 'Review' ? 'Install' : 'Next'}
                </Button>
            </div>
        </form>
    )
}

type PageProps = { setup: Setup; draft: Draft; setDraft: (update: (draft: Draft) => Draft) => void }

function AppsPage({ setup, draft, setDraft }: PageProps) {
    const toggle = (id: string, on: boolean) =>
        setDraft((d) => ({ ...d, apps: on ? [...d.apps, id] : d.apps.filter((app) => app !== id) }))
    const load = useMutation({
        mutationFn: async (file: File) => api.loadSetupPlan(await file.text()),
        onSuccess: (plan) => {
            setDraft(() => draftFromPlan(setup, plan))
            toast.success('Plan loaded. Check each page and type the passwords and keys.')
        },
        onError: (error) => toast.error(error.message),
    })
    return (
        <Card>
            <CardHeader>
                <CardTitle>Choose the apps</CardTitle>
                <CardDescription>
                    Everything is chosen. Leave out what you don't want; you can install it later from Apps.
                </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-wrap items-center gap-2 text-sm">
                <label className={buttonVariants({ variant: 'outline' })}>
                    Load a plan file
                    <input
                        type="file"
                        accept=".yaml,.yml,application/yaml"
                        className="sr-only"
                        onChange={(e) => {
                            const file = e.target.files?.[0]
                            if (file) {
                                load.mutate(file)
                            }
                            e.target.value = ''
                        }}
                    />
                </label>
                <a href={api.setupPlanUrl} download="iglo-setup.yaml" className={buttonVariants({ variant: 'ghost' })}>
                    Download this server's plan
                </a>
                <span className="text-xs text-muted-foreground">
                    A plan file has no passwords or keys. Keep it with your own notes to set up a server again.
                </span>
            </CardContent>
            <CardContent className="grid gap-3 sm:grid-cols-2">
                {setup.apps.map((app) => (
                    <label
                        key={app.id}
                        className={cn(
                            'flex gap-3 rounded-md border p-3',
                            (app.installed || !app.ready) && 'text-muted-foreground',
                        )}
                    >
                        <Checkbox
                            checked={app.installed || draft.apps.includes(app.id)}
                            disabled={app.installed || !app.ready}
                            onCheckedChange={(on) => toggle(app.id, on === true)}
                        />
                        <span className="flex flex-col gap-1 text-sm">
                            <span className="flex items-center gap-2 font-medium text-foreground">
                                {app.name}
                                {app.installed && <Badge variant="outline">Installed</Badge>}
                            </span>
                            <span>{app.description}</span>
                            <span className="text-xs">
                                Shares: {app.shares.map((role) => shareNames[role]).join(', ')}
                            </span>
                            {!app.ready && (
                                <span className="text-xs text-warning">
                                    Prepare the host first: {app.missing.join(', ')}. See the Gaming docs.
                                </span>
                            )}
                        </span>
                    </label>
                ))}
            </CardContent>
        </Card>
    )
}

function NASPage({ setup, draft, setDraft, roles }: PageProps & { roles: string[] }) {
    const nas = draft.nas
    const setNAS = (update: Partial<Draft['nas']>) => setDraft((d) => ({ ...d, nas: { ...d.nas, ...update } }))
    const setShare = (role: string, update: Partial<Draft['nas']['shares'][string]>) =>
        setNAS({ shares: { ...nas.shares, [role]: { ...nas.shares[role], ...update } } })
    const open = roles.filter((role) => !connectedShare(role, setup.bindings))

    return (
        <Card>
            <CardHeader>
                <CardTitle>Shares on the NAS</CardTitle>
                <CardDescription>
                    Each app gets only its own folders. On an Umbrel, open Settings &gt; File sharing, choose Specific
                    folders and add each folder below. They all use the one SMB login, umbrel, shown on that page.
                </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-5">
                {open.length > 0 && (
                    <>
                        <Field
                            id="nas-server"
                            label="NAS address"
                            help="Use its IP address. The host can't look up names like umbrel.local."
                        >
                            <Input
                                id="nas-server"
                                required
                                placeholder="192.168.1.20"
                                value={nas.server}
                                onChange={(e) => setNAS({ server: e.target.value.trim() })}
                            />
                        </Field>
                        <label className="flex items-center justify-between gap-4 text-sm">
                            <span>
                                One account for all shares
                                <span className="block text-xs text-muted-foreground">
                                    Easier, but then every app can reach every share. Separate accounts keep each app to
                                    its own folders. An Umbrel has only one login, so turn this on there.
                                </span>
                            </span>
                            <Switch checked={nas.oneAccount} onCheckedChange={(on) => setNAS({ oneAccount: on })} />
                        </label>
                        {nas.oneAccount && (
                            <Account
                                id="nas-account"
                                value={nas.account}
                                onChange={(account) => setNAS({ account: { ...nas.account, ...account } })}
                            />
                        )}
                    </>
                )}
                {roles.map((role) => {
                    const connected = connectedShare(role, setup.bindings)
                    return (
                        <fieldset key={role} className="flex flex-col gap-3 border-t pt-4">
                            <legend className="section-title">
                                {shareNames[role]}
                                {readOnlyRoles.has(role) ? ' (read only)' : ' (write access)'}
                            </legend>
                            {connected ? (
                                <p className="text-sm text-muted-foreground">
                                    Uses the connected share {connected.id} ({connected.source}).
                                </p>
                            ) : (
                                <div className="grid gap-4 sm:grid-cols-3">
                                    <Field id={`share-${role}`} label="Share">
                                        <Input
                                            id={`share-${role}`}
                                            required
                                            value={nas.shares[role].share}
                                            onChange={(e) => setShare(role, { share: e.target.value.trim() })}
                                        />
                                    </Field>
                                    {!nas.oneAccount && (
                                        <Account
                                            id={`share-${role}`}
                                            value={nas.shares[role]}
                                            onChange={(account) => setShare(role, account)}
                                        />
                                    )}
                                </div>
                            )}
                        </fieldset>
                    )
                })}
                {draft.apps.length > 0 && (
                    <label className="flex items-center justify-between gap-4 border-t pt-4 text-sm">
                        <span>
                            Keep Proxmox backups on the NAS
                            <span className="block text-xs text-muted-foreground">
                                The backups of the containers go to the Backups share, not to the disk of the host. A
                                new backup job runs every night at 03:00; a schedule you set before stays.
                            </span>
                        </span>
                        <Switch
                            checked={draft.proxmoxBackups}
                            onCheckedChange={(on) => setDraft((d) => ({ ...d, proxmoxBackups: on }))}
                        />
                    </label>
                )}
            </CardContent>
        </Card>
    )
}

function Account({
    id,
    value,
    onChange,
}: {
    id: string
    value: { username: string; password: string }
    onChange: (account: Partial<{ username: string; password: string }>) => void
}) {
    return (
        <>
            <Field id={`${id}-username`} label="Username">
                <Input
                    id={`${id}-username`}
                    required
                    autoComplete="off"
                    value={value.username}
                    onChange={(e) => onChange({ username: e.target.value.trim() })}
                />
            </Field>
            <Field id={`${id}-password`} label="Password">
                <Input
                    id={`${id}-password`}
                    type="password"
                    required
                    autoComplete="new-password"
                    value={value.password}
                    onChange={(e) => onChange({ password: e.target.value })}
                />
            </Field>
        </>
    )
}

function AccountsPage({ draft, setDraft, wants }: Omit<PageProps, 'setup'> & { wants: (app: string) => boolean }) {
    const setAdmin = (update: Partial<Draft['admin']>) => setDraft((d) => ({ ...d, admin: { ...d.admin, ...update } }))
    const setVPN = (update: Partial<Draft['vpn']>) => setDraft((d) => ({ ...d, vpn: { ...d.vpn, ...update } }))
    return (
        <Card>
            <CardHeader>
                <CardTitle>Accounts and keys</CardTitle>
                <CardDescription>
                    One login for Jellyfin, Seerr, the apps of the media stack and Immich. Change them in each app later
                    if you like.
                </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-5">
                <div className="grid gap-4 sm:grid-cols-2">
                    <Field id="admin-username" label="Username" help="Letters, digits, dots, dashes and underscores.">
                        <Input
                            id="admin-username"
                            required
                            pattern="[A-Za-z0-9._\-]{1,32}"
                            autoComplete="username"
                            value={draft.admin.username}
                            onChange={(e) => setAdmin({ username: e.target.value.trim() })}
                        />
                    </Field>
                    {wants('photos') && (
                        <Field id="admin-email" label="Email" help="Immich logs in with an email address.">
                            <Input
                                id="admin-email"
                                type="email"
                                required
                                value={draft.admin.email}
                                onChange={(e) => setAdmin({ email: e.target.value.trim() })}
                            />
                        </Field>
                    )}
                    {wants('photos') && (
                        <label className="flex items-start gap-3 rounded-md border p-3 text-sm">
                            <Checkbox
                                checked={draft.allowRestrictedFaceModels}
                                onCheckedChange={(on) =>
                                    setDraft((d) => ({ ...d, allowRestrictedFaceModels: on === true }))
                                }
                            />
                            <span>
                                <span className="font-medium">Enable face grouping</span>
                                <span className="mt-1 block text-muted-foreground">
                                    Immich's default face model has non-commercial research terms for its weights. Leave
                                    this off for FOSS defaults. This only configures a new Photos library.
                                </span>
                            </span>
                        </label>
                    )}
                    <Field id="admin-password" label="Password" help="At least 12 characters.">
                        <Input
                            id="admin-password"
                            type="password"
                            required
                            minLength={12}
                            autoComplete="new-password"
                            value={draft.admin.password}
                            onChange={(e) => setAdmin({ password: e.target.value })}
                        />
                    </Field>
                    <Field id="admin-confirm" label="Password again">
                        <Input
                            id="admin-confirm"
                            type="password"
                            required
                            autoComplete="new-password"
                            value={draft.confirm}
                            onChange={(e) => setDraft((d) => ({ ...d, confirm: e.target.value }))}
                        />
                    </Field>
                </div>

                {wants('photos') && (
                    <fieldset className="flex flex-col gap-4 border-t pt-4">
                        <legend className="section-title">Photos backups</legend>
                        <BackupPasswordFields
                            id="setup-backup-password"
                            password={draft.backupPassword}
                            confirm={draft.backupConfirm}
                            problem=""
                            onPassword={(backupPassword) => setDraft((d) => ({ ...d, backupPassword }))}
                            onConfirm={(backupConfirm) => setDraft((d) => ({ ...d, backupConfirm }))}
                        />
                    </fieldset>
                )}

                {wants('media') && (
                    <fieldset className="flex flex-col gap-4 border-t pt-4">
                        <legend className="section-title">Media stack</legend>
                        <VpnFields
                            id="setup-vpn"
                            value={{
                                provider: draft.vpn.provider,
                                locations: draft.vpn.locations,
                                wireguardPrivateKey: draft.vpn.wireguardKey ?? '',
                                piaUsername: draft.vpn.piaUsername ?? '',
                                piaPassword: draft.vpn.piaPassword ?? '',
                                piaDedicatedToken: draft.vpn.piaDedicatedToken ?? '',
                            }}
                            onChange={({ wireguardPrivateKey, ...patch }) =>
                                setVPN(
                                    wireguardPrivateKey === undefined
                                        ? patch
                                        : { ...patch, wireguardKey: wireguardPrivateKey },
                                )
                            }
                        />
                        <Toggle
                            label="Anime trackers"
                            help="Adds Nyaa.si and Anime Tosho to Prowlarr. Anime always gets a quality profile in Sonarr and Radarr that rejects dubbed-only releases, and one download per episode."
                            checked={draft.animeIndexers}
                            onChange={(on) => setDraft((d) => ({ ...d, animeIndexers: on }))}
                        />
                        <Toggle
                            label="FlareSolverr"
                            help="For indexers behind Cloudflare. It runs a browser that can take up to 1 GB. You can turn it on or off later on the Apps page."
                            checked={draft.flareSolverr}
                            onChange={(on) => setDraft((d) => ({ ...d, flareSolverr: on }))}
                        />
                        <p className="text-xs text-muted-foreground">
                            The Videos share gets the folders movies, series, anime and anime-movies, each with a
                            library in Jellyfin. Subtitles are English; change the languages in Bazarr.
                        </p>
                    </fieldset>
                )}

                {wants('gaming') && (
                    <label className="flex gap-3 border-t pt-4 text-sm">
                        <Checkbox
                            required
                            checked={draft.confirmPrivileged}
                            onCheckedChange={(on) => setDraft((d) => ({ ...d, confirmPrivileged: on === true }))}
                        />
                        <span>
                            Gaming runs in a privileged container, as Wolf needs. Treat it like software on the host
                            itself.
                        </span>
                    </label>
                )}
            </CardContent>
        </Card>
    )
}

function Toggle({
    label,
    help,
    checked,
    onChange,
}: {
    label: string
    help: string
    checked: boolean
    onChange: (on: boolean) => void
}) {
    return (
        <label className="flex items-center justify-between gap-4 text-sm">
            <span>
                {label}
                <span className="block text-xs text-muted-foreground">{help}</span>
            </span>
            <Switch checked={checked} onCheckedChange={onChange} />
        </label>
    )
}

function ReviewPage({ setup, draft, roles }: { setup: Setup; draft: Draft; roles: string[] }) {
    const chosen = setup.apps.filter((app) => draft.apps.includes(app.id))
    const always = chosen.filter((app) => app.id !== 'gaming')
    const gaming = chosen.find((app) => app.id === 'gaming')
    // The manager runs too, in 512 MiB.
    const limitBytes = (always.reduce((sum, app) => sum + app.memoryMiB, 0) + 512) * 2 ** 20
    const peakBytes = limitBytes + (gaming ? gaming.memoryMiB * 2 ** 20 : 0)
    const tight = setup.hostBytes > 0 && peakBytes > setup.hostBytes
    // Installed apps have their disks already. The disks are thin: they take
    // space as the apps write, but together they can't outgrow the storage.
    const diskBytes = chosen.filter((app) => !app.installed).reduce((sum, app) => sum + app.diskGiB * 2 ** 30, 0)
    const space = setup.storageSpace?.[draft.storage]

    return (
        <Card>
            <CardHeader>
                <CardTitle>Check before you install</CardTitle>
                <CardDescription>
                    Nothing has changed yet. Install runs everything below, in this order.
                </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4 text-sm">
                <Row label="Shares">
                    {roles
                        .map((role) => {
                            const connected = connectedShare(role, setup.bindings)
                            return connected
                                ? `${shareNames[role]} (uses ${connected.id})`
                                : `${shareNames[role]} (//${draft.nas.server}/${draft.nas.shares[role].share})`
                        })
                        .join(', ')}
                </Row>
                <Row label="NAS accounts">
                    {draft.nas.oneAccount ? `One account, ${draft.nas.account.username}` : 'One account per share'}
                </Row>
                {draft.proxmoxBackups && (
                    <Row label="Proxmox backups">On the Backups share; a new job runs every night at 03:00</Row>
                )}
                <Row label="Apps">{chosen.map((app) => app.name).join(', ')}</Row>
                {draft.apps.includes('media') && (
                    <Row label="VPN">
                        {draft.vpn.provider === 'pia' ? 'PIA' : 'ProtonVPN'},{' '}
                        {draft.vpn.locations.split(',').join(', ')}
                    </Row>
                )}
                <Row label="Login">
                    {draft.admin.username}
                    {draft.apps.includes('photos') && `, and ${draft.admin.email} for Immich`}
                </Row>
                <Row label="Memory">
                    {formatBytes(limitBytes)} in limits for the apps that always run
                    {gaming && `, ${formatBytes(peakBytes)} while you play`}
                    {setup.hostBytes > 0 && `, of ${formatBytes(setup.hostBytes)} on the host`}. Limits are not
                    reservations: idle apps use far less.
                </Row>
                <Row label="Disk">
                    {formatBytes(diskBytes)} of container disks on {draft.storage}
                    {space &&
                        `, which holds ${formatBytes(space.totalBytes)} with ${formatBytes(space.freeBytes)} free`}
                    .
                    {draft.apps.includes('media') &&
                        ` The media stack takes ${formatBytes(200 * 2 ** 30)} of it for downloads.`}
                </Row>
                {space && diskBytes > space.totalBytes && (
                    <p className="text-destructive">
                        These disks are more than {draft.storage} holds. They would fill it before they are full, and
                        then every container on it stops writing. Leave out an app, or choose another storage.
                    </p>
                )}
                {space && diskBytes <= space.totalBytes && diskBytes > space.freeBytes && (
                    <p className="text-warning">
                        These disks are more than is free on {draft.storage}. They only take space as the apps write, so
                        this works until they fill up. Keep an eye on the disk on the Health page.
                    </p>
                )}
                {tight && (
                    <p className="text-warning">
                        All limits together are more than the memory of the host. That works while the apps stay below
                        their limits, but leaves no room for everything at its peak.
                    </p>
                )}
            </CardContent>
            <CardFooter className="text-xs text-muted-foreground">
                Passwords and keys stay in the memory of the manager until the setup is done. They are never written to
                its disk.
            </CardFooter>
        </Card>
    )
}

function Row({ label, children }: { label: string; children: ReactNode }) {
    return (
        <div className="grid gap-1 sm:grid-cols-[10rem_1fr]">
            <span className="text-muted-foreground">{label}</span>
            <span>{children}</span>
        </div>
    )
}
