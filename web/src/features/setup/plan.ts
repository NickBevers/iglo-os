import type { Setup, SetupPlan, StorageBinding } from '@/lib/api'

// The order of the shares on the NAS page, with the name the share usually has.
export const shareNames: Record<string, string> = {
    videos: 'Videos',
    photos: 'Photos',
    backups: 'Backups',
    music: 'Music',
    inbox: 'Inbox',
    games: 'Games',
}

// readOnlyRoles are the shares that the apps only read.
export const readOnlyRoles = new Set(['games', 'inbox'])

// Draft is the plan while you fill it in. confirm repeats the admin password.
export type Draft = SetupPlan & { confirm: string }

export function newDraft(setup: Setup): Draft {
    const shares: Draft['nas']['shares'] = {}
    for (const [role, name] of Object.entries(shareNames)) {
        shares[role] = { share: name, username: '', password: '' }
    }
    return {
        // Everything that the host is ready for, and isn't installed yet.
        apps: setup.apps.filter((app) => app.ready && !app.installed).map((app) => app.id),
        nas: { server: '', oneAccount: false, account: { username: '', password: '' }, shares },
        admin: { username: '', password: '', email: '' },
        confirm: '',
        vpn: {
            provider: 'protonvpn',
            locations: 'Netherlands',
            wireguardKey: '',
            piaUsername: '',
            piaPassword: '',
            piaDedicatedToken: '',
        },
        flareSolverr: true,
        animeIndexers: true,
        proxmoxBackups: true,
        confirmPrivileged: false,
        timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
        storage: setup.storage,
    }
}

// rolesOf are the shares that the draft needs, like the manager counts them.
export function rolesOf(draft: Draft, setup: Setup): string[] {
    const needed = new Set<string>()
    for (const app of setup.apps) {
        if (draft.apps.includes(app.id)) {
            app.shares.forEach((role) => needed.add(role))
        }
    }
    if (draft.proxmoxBackups) {
        needed.add('backups')
    }
    return Object.keys(shareNames).filter((role) => needed.has(role))
}

// connectedShare is a share of role that the setup uses again, if there is one.
export function connectedShare(role: string, bindings: StorageBinding[]) {
    return bindings.find((b) => b.role === role && b.mounted && (readOnlyRoles.has(role) || !b.readOnly))
}

// draftFromPlan fills the pages from a plan file. The file has no passwords or
// keys, so those stay empty, and so does the confirmation for Gaming.
export function draftFromPlan(setup: Setup, plan: SetupPlan): Draft {
    const base = newDraft(setup)
    const shares = { ...base.nas.shares }
    for (const [role, share] of Object.entries(plan.nas?.shares ?? {})) {
        if (role in shares) {
            shares[role] = { share: share.share ?? shares[role].share, username: share.username ?? '', password: '' }
        }
    }
    const ready = new Set(setup.apps.filter((app) => app.ready).map((app) => app.id))
    return {
        ...base,
        apps: (plan.apps ?? []).filter((app) => ready.has(app)),
        nas: {
            server: plan.nas?.server ?? '',
            oneAccount: plan.nas?.oneAccount ?? false,
            account: { username: plan.nas?.account?.username ?? '', password: '' },
            shares,
        },
        admin: { username: plan.admin?.username ?? '', password: '', email: plan.admin?.email ?? '' },
        // The plan file has no secrets and no PIA username: the form asks again.
        vpn: {
            ...base.vpn,
            provider: plan.vpn?.provider || base.vpn.provider,
            locations: plan.vpn?.locations || base.vpn.locations,
        },
        flareSolverr: plan.flareSolverr ?? base.flareSolverr,
        animeIndexers: plan.animeIndexers ?? base.animeIndexers,
        proxmoxBackups: plan.proxmoxBackups ?? base.proxmoxBackups,
        timezone: plan.timezone || base.timezone,
        storage: setup.storages.includes(plan.storage) ? plan.storage : base.storage,
    }
}

// planOf is what goes to the manager, without the repeated password.
export function planOf(draft: Draft): SetupPlan {
    const plan: Partial<Draft> = { ...draft }
    delete plan.confirm
    return plan as SetupPlan
}
