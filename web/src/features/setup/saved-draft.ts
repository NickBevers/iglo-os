import type { Setup, SetupPlan } from '@/lib/api'
import { draftFromPlan, type Draft } from './plan'

// The wizard keeps its answers in sessionStorage, so a reload or an expired
// session doesn't clear a long form. Passwords, keys and the VPN login are not
// kept, like the plan file leaves them out: they stay in memory only. The
// storage ends with the tab, and in a private window every access can throw.
const key = 'iglo-setup-draft'

type Saved = { plan: SetupPlan; page: string }

// withoutSecrets lists each field that may be kept. A new field of SetupPlan
// doesn't compile here until someone decides whether it is a secret.
function withoutSecrets(draft: Draft): SetupPlan {
    const shares: SetupPlan['nas']['shares'] = {}
    for (const [role, share] of Object.entries(draft.nas.shares)) {
        shares[role] = { share: share.share, username: share.username, password: '' }
    }
    return {
        apps: draft.apps,
        nas: {
            server: draft.nas.server,
            oneAccount: draft.nas.oneAccount,
            account: { username: draft.nas.account.username, password: '' },
            shares,
        },
        admin: { username: draft.admin.username, password: '', email: draft.admin.email },
        vpn: { provider: draft.vpn.provider, locations: draft.vpn.locations },
        flareSolverr: draft.flareSolverr,
        animeIndexers: draft.animeIndexers,
        proxmoxBackups: draft.proxmoxBackups,
        allowRestrictedFaceModels: draft.allowRestrictedFaceModels,
        // A privileged container is confirmed again after a reload.
        confirmPrivileged: false,
        timezone: draft.timezone,
        storage: draft.storage,
        backupPassword: '',
    }
}

export function saveDraft(draft: Draft, page: string) {
    try {
        sessionStorage.setItem(key, JSON.stringify({ plan: withoutSecrets(draft), page } satisfies Saved))
    } catch {
        // Without storage the wizard still works; a reload starts over.
    }
}

// loadDraft rebuilds the saved answers like a loaded plan file, so apps that
// are not ready anymore and unknown storages drop out.
export function loadDraft(setup: Setup): { draft: Draft; page: string } | null {
    try {
        const text = sessionStorage.getItem(key)
        if (!text) {
            return null
        }
        const saved = JSON.parse(text) as Saved
        return { draft: draftFromPlan(setup, saved.plan), page: saved.page }
    } catch {
        return null
    }
}

export function forgetDraft() {
    try {
        sessionStorage.removeItem(key)
    } catch {
        // Nothing was kept.
    }
}
