export type HeadscaleAnswers = {
    hostname: string
    address: string
    gateway: string
    storage: string
    rateMB: number
    prerequisites: boolean
}
export type InboxProfile = 'preserve' | 'compatible'
export interface InboxJob {
    id: string
    path: string
    profile: InboxProfile
    state: string
    error?: string
    file?: string
    createdAt: string
}
export interface InboxQueue {
    storageReady: boolean
    paused: boolean
    admitted: boolean
    profile: InboxProfile
    gpu: boolean
    jobs: InboxJob[]
    scanError?: string
    sample?: boolean
}
export class ApiError extends Error {
    status: number

    constructor(status: number, message: string) {
        super(message)
        this.status = status
    }
}

export type Usage = {
    cpu: number
    maxCpu: number
    mem: number
    maxMem: number
    disk: number
    maxDisk: number
    uptime: number
}

export type ProxmoxNode = Usage & {
    name: string
    status: string
    pveVersion: string
}

export type Guest = Usage & {
    id: string
    vmid: number
    type: 'lxc' | 'qemu'
    node: string
    name: string
    status: string
}

export type Overview = {
    nodes: ProxmoxNode[]
    guests: Guest[]
    agent: { connected: boolean; hostname?: string; version?: string }
}

export type Timeframe = 'hour' | 'day' | 'week' | 'month' | 'year'

// One sample of the Proxmox statistics. A value is null when Proxmox has no data, like when a guest was stopped.
export type UsagePoint = {
    time: number
    cpu: number | null
    mem: number | null
    maxMem: number | null
    netIn: number | null
    netOut: number | null
}

export type UsageTarget = { node: string; type?: Guest['type']; vmid?: number }

export type Session = { username: string }
export type MusicJob = {
    id: string
    url: string
    video: boolean
    state: string
    error?: string
    file?: string
    createdAt: string
    finishedAt?: string
}
export type MusicQueue = {
    paused: boolean
    admitted: boolean
    storageReady?: boolean
    jobs: MusicJob[]
    sample?: boolean
}
// ImmichAdmin is the first admin that Iglo-OS makes in a new Photos.
export type ImmichAdmin = {
    email: string
    name: string
    password: string
    // faceModels turns face grouping on. Its model weights have non-commercial terms.
    faceModels: boolean
}

export type ScopedAnswers = {
    storage: string
    libraryBinding: string
    outputBinding: string
    backupBinding: string
    cores: number
    memoryMiB: number
    diskGB: number
    // privileged confirms that Gaming may create a privileged container. Only Gaming sets it.
    privileged?: boolean
    // backupPassword is the password of the Photos backups. The host seals it
    // and never sends it back.
    backupPassword?: string
}

// Gaming is the state of the Gaming app. The library counts the games per
// system on the Games share, also while the container is stopped.
// GamingPairing is a Moonlight device that waits to pair. Its pair secret
// stays on the host; id stands for it.
export type GamingPairing = {
    id: string
    clientIp: string
}

export type Gaming = {
    installed: boolean
    vmid?: number
    status?: string
    // address is the IP to add as a host in Moonlight, while the container runs.
    address?: string
    library: { label: string; value: number }[]
    libraryError?: string
    sample?: boolean
    // sessions are the Moonlight streams that run now, one at most.
    sessions: GamingSession[]
    protected: boolean
    protectedUntil?: string
}

export type GamingSession = {
    id: string
    clientIP: string
    width: number
    height: number
    fps: number
}

// GameSystem is one system folder on the Games share. A game's name is its
// file name without the extension; the title drops tags like (USA).
export type GameSystem = {
    id: string
    label: string
    games: { name: string; title: string; cover: boolean }[]
}

// coverURL is the address of a game's cover. The server tries your own cover
// first, then libretro.
export function coverURL(system: string, game: GameSystem['games'][number]) {
    return `/api/gaming/covers/${encodeURIComponent(system)}/${encodeURIComponent(game.name)}${game.cover ? '?own=1' : ''}`
}

export type BindingInput = {
    id: string
    role: string
    server: string
    share: string
    username: string
    password: string
    readOnly: boolean
}
// SetupApp is an app on the first page of the guided setup.
export type SetupApp = {
    id: string
    name: string
    description: string
    installed: boolean
    // shares are the roles of the NAS shares that the app needs.
    shares: string[]
    memoryMiB: number
    // diskGiB is the local disk the app takes, for the review.
    diskGiB: number
    // ready is false when the host isn't prepared yet, with what is missing.
    ready: boolean
    missing: string[]
}

export type SetupStep = {
    id: string
    title: string
    state: 'waiting' | 'running' | 'done' | 'failed' | 'skipped'
    message?: string
    // app is the app that the step installs, for its live log.
    app?: string
}

export type Setup = {
    state: 'idle' | 'running' | 'paused' | 'done'
    steps: SetupStep[]
    apps: SetupApp[]
    hostBytes: number
    bindings: StorageBinding[]
    storage: string
    storages: string[]
    // storageSpace is the size of each storage, for the disk budget.
    storageSpace: Record<string, StorageSpace>
}

// StorageSpace is the size of a storage for container disks, and what is free.
export type StorageSpace = { totalBytes: number; freeBytes: number }

export type SetupAccount = { username: string; password: string }

// SetupPlan is what the setup page sends. Its secrets stay in the memory of the manager.
export type SetupPlan = {
    apps: string[]
    nas: {
        server: string
        oneAccount: boolean
        account: SetupAccount
        shares: Record<string, { share: string } & SetupAccount>
    }
    admin: { username: string; password: string; email: string }
    vpn: {
        provider: VpnProvider
        locations: string
        wireguardKey?: string
        piaUsername?: string
        piaPassword?: string
        piaDedicatedToken?: string
    }
    flareSolverr: boolean
    animeIndexers: boolean
    proxmoxBackups: boolean
    allowRestrictedFaceModels: boolean
    confirmPrivileged: boolean
    timezone: string
    storage: string
    // backupPassword is the password of the Photos backups. Like the other
    // passwords, it is never kept in a plan file or the saved draft.
    backupPassword: string
}

export type StorageBinding = {
    id: string
    role: string
    source: string
    path: string
    readOnly: boolean
    mounted: boolean
    consumers: string[]
    // proxmoxStorage is the Proxmox storage that keeps its backups on this share.
    proxmoxStorage?: string
}

export type WorkloadSettings = {
    workers?: boolean
    enabled: boolean
    url: string
    apiKey?: string
    node: string
    reserveGiB: number
    maxQueues: number
    queues: string[]
    window: boolean
    start: string
    end: string
    timezone: string
}

export type Workloads = {
    workerReason?: string
    workerError?: string
    settings: WorkloadSettings
    configured: boolean
    gamingUntil?: string
    gamingActive: boolean
    checkedAt?: string
    reason: string
    error?: string
    freeBytes: number
    totalBytes: number
    // pressure is the share of the last 10 seconds in which tasks waited for memory, or -1.
    pressure: number
    queues: {
        name: string
        isPaused: boolean
        owned: boolean
        statistics: { active: number; waiting: number; paused: number; failed: number }
    }[]
}

export type GuestAction = 'start' | 'shutdown' | 'reboot' | 'stop'

export type Package = { name: string; from: string; to: string }

export type UpdateTarget = {
    vmid?: number
    name: string
    checkedAt?: string
    packages: Package[]
    images: { service: string; image: string }[]
    unsupported?: boolean
    rebootRequired?: boolean
    error?: string
}

export type GuestUpdates = UpdateTarget & {
    vmid: number
    status: string
    autoUpdate: boolean
    self: boolean
}

export type Schedule = { enabled: boolean; weekday: number; hour: number; minute: number }

export type UpdateRun = {
    id: string
    kind: 'check' | 'host-update' | 'guest-update'
    target: string
    vmid?: number
    scheduled: boolean
    status: 'succeeded' | 'failed' | 'rolled-back'
    snapshot?: string
    message: string
    log?: string
    startedAt: string
    finishedAt: string
}

export type Updates = {
    busy: string
    schedule: Schedule
    nextRun: string | null
    ntfy: { configured: boolean; server: string; topic: string; hasToken: boolean }
    host: UpdateTarget
    guests: GuestUpdates[]
    history: UpdateRun[]
}

export type JellyfinNowPlaying = {
    user: string
    device: string
    itemId: string
    title: string
    subtitle: string
    progress: number
    paused: boolean
    transcode: boolean
    playMethod: string
    reasons: string[]
}

export type Jellyfin = {
    configured: boolean
    url: string
    error?: string
    serverName?: string
    version?: string
    movies: number
    series: number
    episodes: number
    albums: number
    songs: number
    musicVideos: number
    nowPlaying: JellyfinNowPlaying[]
    recent: { id: string; name: string; type: string; year?: number }[]
}

export type UpdateSettings = {
    schedule: Schedule
    excluded: number[]
    // An empty token keeps the saved one while the server stays the same.
    ntfy: { server: string; topic: string; token: string }
}

export type CheckKind = 'http' | 'tcp'

export type CheckInput = { name: string; kind: CheckKind; target: string }

export type ServiceCheck = CheckInput & {
    id: string
    status: 'pending' | 'up' | 'down'
    latencyMs: number
    error?: string
    checkedAt?: string
    since?: string
}

export type Disk = {
    node: string
    devPath: string
    model: string
    serial: string
    size: number
    type: string
    used: string
    health: string
    wearout: number | null
    problem?: string
}

export type ZFSPool = {
    node: string
    name: string
    health: string
    size: number
    alloc: number
    frag: number
    problem?: string
}

export type Storage = {
    node: string
    name: string
    status: string
    used: number
    total: number
    problem?: string
}

export type Share = {
    path: string
    source: string
    fsType: string
    mounted: boolean
    size: number
    used: number
    error?: string
    problem?: string
}

export type Health = {
    services: ServiceCheck[]
    disks: Disk[]
    pools: ZFSPool[]
    storage: Storage[]
    shares: Share[]
    errors: string[]
    checkedAt?: string
}

export type UpgradeStatus = {
    state: 'running' | 'succeeded' | 'failed'
    version?: string
    message?: string
    startedAt?: string
    finishedAt?: string
    log?: string
}

// SelfUpdate is the running version and the last upgrade, which came from
// "make deploy" on the owner's machine.
export type SelfUpdate = {
    version: string
    upgrade: UpgradeStatus | null
}

export type TailscaleSettings = { shareSubnet: boolean; subnet: string; useTag?: boolean; loginServer?: string }

export type TailscaleService = { name: string; title: string; published: boolean; url?: string }

export type Tailscale = {
    installed: boolean
    state: string
    authUrl?: string
    connecting: boolean
    error?: string
    health: string[]
    name?: string
    dnsName?: string
    ips: string[]
    tailnet?: string
    serving: boolean
    serveUrl?: string
    tags: string[]
    services: TailscaleService[]
    settings: TailscaleSettings
    suggestedSubnet?: string
    subnetApproved: boolean
    peers: { name: string; dnsName: string; os: string; ips: string[]; online: boolean; lastSeen?: string }[]
}

export type BackupJob = {
    enabled: boolean
    days: string[]
    hour: number
    minute: number
    storage: string
    exclude: number[]
    keepDaily: number
    keepWeekly: number
    keepMonthly: number
}

export type GuestBackup = {
    volid: string
    storage: string
    createdAt: string
    size: number
    notes: string
    protected: boolean
}

export type BackupRun = {
    id: string
    kind: 'backup' | 'restore' | 'delete' | 'check'
    vmid: number
    target: string
    succeeded: boolean
    message: string
    startedAt: string
    finishedAt: string
    // A backup that the Proxmox job ran, not Iglo-OS.
    scheduled?: boolean
}

export type Backups = {
    busy: string
    job: BackupJob & { exists: boolean; nextRun?: string; custom?: string }
    storages: { node: string; name: string; type: string; total: number; used: number }[]
    guests: {
        vmid: number
        name: string
        type: 'lxc' | 'qemu'
        status: string
        included: boolean
        self: boolean
        backups: GuestBackup[]
    }[]
    history: BackupRun[]
    // check is the monthly restore check: the last one, and when the next is due.
    check: { last?: BackupRun; next: string }
}

export type LogEntry = { time: string; level: number; source: string; message: string }

export type TaskEntry = LogEntry & { node: string; upid: string }

export type DockerLogs = {
    // installed is false when the container has no Docker, like the manager.
    installed: boolean
    containers: { name: string; state: string; image: string }[]
    entries: LogEntry[]
}

export type AppOperation = {
    app?: string
    action?: 'install' | 'update' | 'remove' | 'vpn' | 'flaresolverr' | 'backup' | 'verify'
    state: 'idle' | 'running' | 'succeeded' | 'failed'
    message?: string
    startedAt?: string
    finishedAt?: string
    vmid?: number
    ip?: string
    log?: string
}

export type JellyfinAnswers = {
    // videosBinding is the Videos share from Storage.
    videosBinding: string
    moviesFolder: string
    seriesFolder: string
    // animeFolder and animeMoviesFolder are optional. Empty keeps anime in
    // the series and movies folders.
    animeFolder: string
    animeMoviesFolder: string
    // musicBinding is the Music share from Storage, or empty for none. Saved
    // answers from before it have none.
    musicBinding?: string
    adminUsername: string
    adminPassword: string
    storage: string
}

export type SavedAnswers = {
    headscale?: HeadscaleAnswers
    scoped: ScopedAnswers
    answers: MediaStackAnswers
    jellyfin: JellyfinAnswers
    hasJellyfinApiKey: boolean
    hasJellyfinAdminPassword: boolean
    hasOpenSubtitlesPassword: boolean
    until: string
}

// AppStats are what an app holds and does right now. A figure with the unit
// bytes is a size. An activity has a progress of 0 to 100, or -1 for none.
export type AppStats = {
    sample?: boolean
    figures: { label: string; value: number; unit?: 'bytes' }[]
    activity: { title: string; detail?: string; progress: number }[]
}

export type CatalogApp = {
    controllerHost?: string
    id: string
    name: string
    description: string
    links: { name: string; description: string; url?: string }[]
    installed: boolean
    // planned is set for an app that Iglo-OS can't install yet.
    planned: boolean
    stats?: AppStats
    version?: string
    updateAvailable: boolean
    hostUrl?: string
    vmid?: number
    status?: string
    operation: AppOperation | null
    rollback?: string
    saved: SavedAnswers | null
    // flareSolverr says whether the media stack runs FlareSolverr. Only set for
    // a media stack that runs.
    flareSolverr?: boolean
    // photosBackup is the settings and the state of the Photos backups.
    photosBackup?: PhotosBackup
}

// PhotosBackupSettings are when the nightly Photos backup runs, how many
// snapshots it keeps, and the folder on a disk of the host for the copy off
// the NAS, or empty.
export type PhotosBackupSettings = {
    time: string
    offsitePath: string
    keepDaily: number
    keepWeekly: number
    keepMonthly: number
}

export type PhotosRepository = {
    bytes?: number
    snapshots?: number
    // result of the last copy off the NAS. missing means its disk can't be used.
    result?: 'ok' | 'failed' | 'missing' | 'off'
    message?: string
    // finished is the last copy off the NAS that worked.
    finished?: string
}

// PhotosBackup is what the host says about the Photos backups. The password
// of the backups never leaves the host: hasPassword only says it is there.
export type PhotosBackup = {
    settings: PhotosBackupSettings
    hasPassword: boolean
    status: {
        started?: string
        finished?: string
        result?: 'ok' | 'failed'
        message?: string
        snapshot?: string
        nas: PhotosRepository
        offsite: PhotosRepository
        verified?: string
        verifyResult?: 'ok' | 'failed'
        verifyMessage?: string
        verifySnapshot?: string
    }
}

export type Apps = {
    apps: CatalogApp[]
    defaults: {
        storages: string[]
        storage: string
        jellyfinVmid?: number
        // mediaVmid is the media stack, when it is installed.
        mediaVmid?: number
        moviesFolder: string
        seriesFolder: string
        animeFolder: string
        animeMoviesFolder: string
        vpnProvider: VpnProvider
        vpnLocations: string
        subtitleLanguages: string
        username: string
        downloadsSize: number
        // The host's address with its prefix and its gateway on vmbr0, for the Headscale form.
        lanAddress?: string
        lanGateway?: string
    }
    error?: string
}

export type VpnProvider = 'protonvpn' | 'pia'

// VpnSettings are the VPN of the media stack. Locations are countries
// (ProtonVPN) or regions (PIA), separated by commas, or "Dedicated IP". When
// changing the VPN, an empty secret keeps the one in the container.
export type VpnSettings = {
    provider: VpnProvider
    locations: string
    wireguardPrivateKey: string
    piaUsername: string
    piaPassword: string
    piaDedicatedToken: string
}

// VpnView is the VPN of an installed media stack, without its secrets. The
// has fields say which secrets the host keeps sealed.
export type VpnView = {
    provider: VpnProvider
    locations: string
    hasWireguardKey: boolean
    hasPiaLogin: boolean
    hasDedicatedToken: boolean
}

export type MediaStackAnswers = {
    // videosBinding is the Videos share from Storage.
    videosBinding: string
    moviesFolder: string
    seriesFolder: string
    // animeFolder and animeMoviesFolder are optional, as for Jellyfin.
    animeFolder: string
    animeMoviesFolder: string
    vpn: VpnSettings
    subtitleLanguages: string
    username: string
    password: string
    jellyfinApiKey: string
    jellyfinAdminUsername: string
    jellyfinAdminPassword: string
    openSubtitlesUsername: string
    openSubtitlesPassword: string
    restartJellyfin: boolean
    // flareSolverr runs FlareSolverr, for indexers behind Cloudflare.
    flareSolverr: boolean
    // animeIndexers adds Nyaa.si and Anime Tosho to Prowlarr.
    animeIndexers: boolean
    storage: string
    downloadsSize: number
}

export type SettingsChange = {
    section: 'notifications' | 'updates' | 'health' | 'backups' | 'tailscale' | 'jellyfin' | 'backgroundWork'
    status: 'unchanged' | 'changed' | 'applied' | 'skipped' | 'failed'
    message?: string
}

export type SettingsImport = { applied: boolean; changes: SettingsChange[] }

async function fail(response: Response): Promise<never> {
    const data = await response.json().catch(() => ({}))
    throw new ApiError(response.status, data.error ?? response.statusText)
}

// downloadDataBackup returns the encrypted backup and its file name.
async function downloadDataBackup(password: string, passphrase: string) {
    const response = await fetch('/api/data-backup/download', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-Iglo-Request': '1' },
        body: JSON.stringify({ password, passphrase }),
    })
    if (!response.ok) {
        return fail(response)
    }

    const name = /filename="([^"]+)"/.exec(response.headers.get('Content-Disposition') ?? '')?.[1]
    return { blob: await response.blob(), name: name ?? 'iglo-data.hlbackup' }
}

async function restoreDataBackup(file: File, password: string, passphrase: string) {
    const form = new FormData()
    form.append('password', password)
    form.append('passphrase', passphrase)
    form.append('file', file)

    const response = await fetch('/api/data-backup/restore', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'X-Iglo-Request': '1' },
        body: form,
    })
    if (!response.ok) {
        return fail(response)
    }
}

// A string body is sent as it is, for example YAML. Anything else is sent as JSON.
async function request<T>(method: string, path: string, body?: unknown, contentType = 'application/json'): Promise<T> {
    const response = await fetch(`/api${path}`, {
        method,
        credentials: 'same-origin',
        headers: { 'Content-Type': contentType, 'X-Iglo-Request': '1' },
        body: body === undefined ? undefined : typeof body === 'string' ? body : JSON.stringify(body),
    })

    if (response.status === 204) {
        return undefined as T
    }

    if (!response.ok) {
        return fail(response)
    }

    return (await response.json().catch(() => ({}))) as T
}

export const api = {
    inboxJobs: () => request<InboxQueue>('GET', '/inbox'),
    submitInbox: (input: { path: string; profile: InboxProfile }) => request<InboxJob>('POST', '/inbox', input),
    inboxSettings: (input: { paused: boolean; profile: InboxProfile }) =>
        request<void>('PUT', '/inbox/settings', input),
    scanInbox: () => request<void>('POST', '/inbox/scan'),
    retryInbox: (id: string) => request<void>('POST', `/inbox/retry/${encodeURIComponent(id)}`),
    musicJobs: () => request<MusicQueue>('GET', '/music/jobs'),
    submitMusic: (input: { url: string; video: boolean }) => request<MusicJob>('POST', '/music/jobs', input),
    pauseMusic: (paused: boolean) => request<void>('PUT', '/music/pause', { paused }),
    photosRecovery: (action: 'backup' | 'verify') => request<void>('POST', `/photos/recovery/${action}`),
    savePhotosBackups: (settings: PhotosBackupSettings) => request<void>('PUT', '/photos/backups', settings),
    installHeadscale: (headscale: HeadscaleAnswers) => request<void>('POST', '/apps/headscale/install', { headscale }),
    // immich makes the first admin of a new Photos. The password stays in the memory of the manager.
    installScopedApp: (id: string, scoped: ScopedAnswers, immich?: ImmichAdmin) =>
        request<void>('POST', `/apps/${id}/install`, { scoped, immich }),
    gaming: () => request<Gaming>('GET', '/gaming'),
    playGaming: (play: boolean) => request<void>('PUT', '/gaming/play', { play }),
    endGamingSession: (id: string) => request<void>('POST', `/gaming/sessions/${encodeURIComponent(id)}/stop`),
    gamingPairings: () => request<GamingPairing[]>('GET', '/gaming/pairing'),
    pairGamingDevice: (id: string, pin: string) =>
        request<void>('POST', `/gaming/pairing/${encodeURIComponent(id)}`, { pin }),
    gamingGames: () => request<{ systems: GameSystem[] }>('GET', '/gaming/games'),
    storageBindings: () => request<{ bindings: StorageBinding[]; sample: boolean }>('GET', '/storage/bindings'),
    setup: () => request<Setup>('GET', '/setup'),
    startSetup: (plan: SetupPlan) => request<void>('POST', '/setup', plan),
    retrySetup: () => request<void>('POST', '/setup/retry'),
    skipSetup: () => request<void>('POST', '/setup/skip'),
    cancelSetup: () => request<void>('DELETE', '/setup'),
    // setupPlanUrl downloads the plan of this server, without passwords and keys.
    setupPlanUrl: '/api/setup/plan',
    // loadSetupPlan reads a plan file. It starts nothing; the wizard fills its pages from it.
    loadSetupPlan: (yaml: string) => request<SetupPlan>('POST', '/setup/plan', yaml, 'application/yaml'),
    createBinding: (input: BindingInput) => request<void>('POST', '/storage/bindings', input),
    deleteBinding: (id: string) => request<void>('DELETE', `/storage/bindings/${encodeURIComponent(id)}`),
    setBindingBackups: (id: string, enabled: boolean) =>
        request<void>('PUT', `/storage/bindings/${encodeURIComponent(id)}/proxmox-backups`, { enabled }),
    workloads: () => request<Workloads>('GET', '/workloads'),
    saveWorkloadSettings: (settings: WorkloadSettings) => request<void>('PUT', '/workloads/settings', settings),
    protectWorkloads: (minutes: number) => request<void>('PUT', '/workloads/protection', { minutes }),
    me: () => request<Session>('GET', '/auth/me'),
    login: (credentials: { username: string; password: string; code: string }) =>
        request<Session>('POST', '/auth/login', credentials),
    logout: () => request<void>('POST', '/auth/logout'),
    overview: () => request<Overview>('GET', '/overview'),
    guestAction: (guest: Guest, action: GuestAction) =>
        request<{ task: string }>('POST', `/guests/${guest.node}/${guest.type}/${guest.vmid}/${action}`),
    usage: ({ node, type, vmid }: UsageTarget, timeframe: Timeframe) =>
        request<{ points: UsagePoint[] }>(
            'GET',
            `/usage/${type ? `${node}/${type}/${vmid}` : node}?${new URLSearchParams({ timeframe })}`,
        ),
    updates: () => request<Updates>('GET', '/updates'),
    updateRun: (id: string) => request<UpdateRun>('GET', `/updates/runs/${id}`),
    checkUpdates: () => request<void>('POST', '/updates/check'),
    updateHost: () => request<void>('POST', '/updates/host'),
    updateGuest: (vmid: number) => request<void>('POST', `/updates/guests/${vmid}`),
    saveUpdateSettings: (settings: UpdateSettings) => request<void>('PUT', '/updates/settings', settings),
    testNotification: () => request<void>('POST', '/updates/test-notification'),
    jellyfin: () => request<Jellyfin>('GET', '/jellyfin'),
    saveJellyfinSettings: (settings: { url: string; apiKey: string }) =>
        request<void>('PUT', '/jellyfin/settings', settings),
    selfUpdate: () => request<SelfUpdate>('GET', '/self-update'),
    backups: () => request<Backups>('GET', '/backups'),
    saveBackupJob: (job: BackupJob) => request<void>('PUT', '/backups/job', job),
    backUpGuest: (vmid: number) => request<void>('POST', `/backups/guests/${vmid}`),
    restoreBackup: (vmid: number, volid: string) => request<void>('POST', '/backups/restore', { vmid, volid }),
    deleteBackup: (volid: string) => request<void>('POST', '/backups/delete', { volid }),
    checkBackup: (volid: string) => request<void>('POST', '/backups/check', { volid }),
    journal: (vmid: number, priority: number, lines = 500) =>
        request<LogEntry[]>(
            'GET',
            `/logs/journal?${new URLSearchParams({ vmid: String(vmid), priority: String(priority), lines: String(lines) })}`,
        ),
    dockerLogs: (vmid: number, lines = 200) =>
        request<DockerLogs>('GET', `/logs/docker?${new URLSearchParams({ vmid: String(vmid), lines: String(lines) })}`),
    tasks: () => request<TaskEntry[]>('GET', '/logs/tasks'),
    taskLog: (node: string, upid: string) =>
        request<{ lines: string[] }>('GET', `/logs/tasks/${node}/log?${new URLSearchParams({ upid })}`),
    tailscale: () => request<Tailscale>('GET', '/tailscale'),
    connectTailscale: (authKey: string) => request<void>('POST', '/tailscale/connect', { authKey }),
    logoutTailscale: () => request<void>('POST', '/tailscale/logout'),
    setTailscaleServe: (enabled: boolean) => request<void>('PUT', '/tailscale/serve', { enabled }),
    saveTailscaleSettings: (settings: TailscaleSettings) => request<void>('PUT', '/tailscale/settings', settings),
    useTailscaleTag: () => request<void>('POST', '/tailscale/tag'),
    setTailscaleService: ({ name, published }: { name: string; published: boolean }) =>
        request<void>('PUT', `/tailscale/services/${name}`, { published }),
    apps: () => request<Apps>('GET', '/apps'),
    installJellyfin: (jellyfin: JellyfinAnswers, keepSecrets = false) =>
        request<void>('POST', '/apps/jellyfin/install', { jellyfin, keepSecrets }),
    installApp: (id: string, answers: MediaStackAnswers, keepSecrets = false) =>
        request<void>('POST', `/apps/${id}/install`, { ...answers, keepSecrets }),
    retryApp: (id: string) => request<void>('POST', `/apps/${id}/retry`),
    forgetAppAnswers: (id: string) => request<void>('DELETE', `/apps/${id}/answers`),
    updateApp: (id: string) => request<void>('POST', `/apps/${id}/update`),
    removeApp: (id: string, confirm?: string) =>
        request<void>('POST', `/apps/${id}/remove`, confirm === undefined ? undefined : { confirm }),
    vpnSettings: (id: string) => request<VpnView>('GET', `/apps/${id}/vpn`),
    changeVpn: (id: string, settings: VpnSettings) => request<void>('PUT', `/apps/${id}/vpn`, settings),
    changeFlareSolverr: (id: string, enabled: boolean) => request<void>('PUT', `/apps/${id}/flaresolverr`, { enabled }),
    settingsExportUrl: '/api/settings/export',
    downloadDataBackup,
    restoreDataBackup,
    importSettings: (yaml: string, apply: boolean) =>
        request<SettingsImport>('POST', `/settings/import${apply ? '?apply=1' : ''}`, yaml, 'application/yaml'),
    health: () => request<Health>('GET', '/health'),
    refreshHealth: () => request<Health>('POST', '/health/refresh'),
    addCheck: (input: CheckInput) => request<ServiceCheck>('POST', '/health/checks', input),
    updateCheck: (id: string, input: CheckInput) => request<void>('PUT', `/health/checks/${id}`, input),
    deleteCheck: (id: string) => request<void>('DELETE', `/health/checks/${id}`),
}

export function jellyfinImage(itemId: string, type: 'Primary' | 'Backdrop') {
    return `/api/jellyfin/items/${itemId}/image?type=${type}`
}
