import { useState, type FormEvent, type ReactNode } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Field } from '@/features/apps/form-parts'
import { VpnFields } from '@/features/apps/vpn-fields'
import { api, type Setup } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { cn } from '@/lib/utils'
import { connectedShare, draftFromPlan, newDraft, planOf, readOnlyRoles, rolesOf, shareNames, type Draft } from './plan'

const pages = ['Apps', 'NAS', 'Accounts', 'Review'] as const
type Page = (typeof pages)[number]

// Wizard asks for everything first, and starts nothing until the review.
export function Wizard({ setup }: { setup: Setup }) {
    const [draft, setDraft] = useState<Draft>(() => newDraft(setup))
    const [page, setPage] = useState<Page>('Apps')
    const client = useQueryClient()
    const start = useMutation({
        mutationFn: () => api.startSetup(planOf(draft)),
        onSuccess: () => client.invalidateQueries({ queryKey: ['setup'] }),
        onError: (error) => toast.error(error.message),
    })
    const roles = rolesOf(draft, setup)
    const wants = (app: string) => draft.apps.includes(app)

    function next(event: FormEvent) {
        event.preventDefault()
        if (page === 'Accounts' && draft.admin.password !== draft.confirm) {
            toast.error('The two admin passwords differ')
            return
        }
        if (page === 'Review') {
            start.mutate()
            return
        }
        setPage(pages[pages.indexOf(page) + 1])
    }

    return (
        <form className="flex flex-col gap-6" onSubmit={next}>
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

            {page === 'Apps' && <AppsPage setup={setup} draft={draft} setDraft={setDraft} />}
            {page === 'NAS' && <NASPage setup={setup} draft={draft} setDraft={setDraft} roles={roles} />}
            {page === 'Accounts' && <AccountsPage draft={draft} setDraft={setDraft} wants={wants} />}
            {page === 'Review' && <ReviewPage setup={setup} draft={draft} roles={roles} />}

            <div className="flex gap-2">
                {page !== 'Apps' && (
                    <Button type="button" variant="outline" onClick={() => setPage(pages[pages.indexOf(page) - 1])}>
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
            {import.meta.env.VITE_DEMO !== '1' && (
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
                    <a
                        href={api.setupPlanUrl}
                        download="iglo-setup.yaml"
                        className={buttonVariants({ variant: 'ghost' })}
                    >
                        Download this server's plan
                    </a>
                    <span className="text-xs text-muted-foreground">
                        A plan file has no passwords or keys. Keep it with your own notes to set up a server again.
                    </span>
                </CardContent>
            )}
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
                    Each app gets only its own folders. On an Umbrel, share each folder in Files and look up its account
                    in Settings &gt; File sharing.
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
                                    its own folders.
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
                            help="For indexers behind Cloudflare. It runs a browser of a few hundred MB."
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
