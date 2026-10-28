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
}

// Gaming is the state of the Gaming app. The library counts the games per
// system on the Games share, also while the container is stopped.
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

// libretroSystems are the folders of the libretro thumbnail server, for the demo.
const libretroSystems: Record<string, string> = {
    gb: 'Nintendo - Game Boy',
    gbc: 'Nintendo - Game Boy Color',
    gba: 'Nintendo - Game Boy Advance',
    n64: 'Nintendo - Nintendo 64',
    gc: 'Nintendo - GameCube',
}

// coverURL is the address of a game's cover. The server tries your own cover
// first, then libretro. The demo has no server, so it asks libretro directly.
export function coverURL(system: string, game: GameSystem['games'][number]) {
    if (import.meta.env.VITE_DEMO === '1') {
        const name = game.name.replace(/[&*/:`<>?\\|"]/g, '_')
        return `https://thumbnails.libretro.com/${encodeURIComponent(libretroSystems[system] ?? '')}/Named_Boxarts/${encodeURIComponent(name)}.png`
    }
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
}

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
    confirmPrivileged: boolean
    timezone: string
    storage: string
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
    // immichGuest is the container with the address of the Immich URL, if any.
    immichGuest?: string
    nodeWarning?: string
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
    telegram: { configured: boolean; chatId: string }
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
    themeEnabled: boolean
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
    telegram: { botToken: string; chatId: string }
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

export type SelfUpdate = {
    version: string
    repo: string
    tokenSet: boolean
    autoInstall: boolean
    latest: { version: string; notes: string; url: string; publishedAt: string } | null
    updateAvailable: boolean
    checkedAt?: string
    error?: string
    installing: boolean
    upgrade: UpgradeStatus | null
}

export type SelfUpdateSettings = { repo: string; token: string; clearToken: boolean; autoInstall: boolean }

export type TailscaleSettings = { shareSubnet: boolean; subnet: string; useTag?: boolean }

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
    kind: 'backup' | 'restore' | 'delete'
    vmid: number
    target: string
    succeeded: boolean
    message: string
    startedAt: string
    finishedAt: string
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
    action?: 'install' | 'update' | 'remove' | 'vpn' | 'players' | 'backup' | 'verify'
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
    adminUsername: string
    adminPassword: string
    theme: boolean
    storage: string
}

export type MinecraftAnswers = {
    acceptEula: boolean
    // memory is in GB.
    memory: number
    motd: string
    difficulty: 'peaceful' | 'easy' | 'normal' | 'hard'
    mode: 'survival' | 'creative' | 'adventure'
    maxPlayers: number
    viewDistance: number
    seed: string
    operator: string
    whitelist: string[]
    playitSecretKey: string
    publicAddress: string
    storage: string
    worldSize: number
}

export type MinecraftPlayers = { whitelist: string[]; operators: string[] }

export type SavedAnswers = {
    scoped: ScopedAnswers
    answers: MediaStackAnswers
    jellyfin: JellyfinAnswers
    minecraft: MinecraftAnswers
    hasJellyfinApiKey: boolean
    hasJellyfinAdminPassword: boolean
    hasOpenSubtitlesPassword: boolean
    hasPlayitSecretKey: boolean
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
    id: string
    name: string
    description: string
    // A link with address is not a website, like a game server: url is then ip:port.
    links: { name: string; description: string; address?: boolean; url?: string }[]
    installed: boolean
    // managed is set when Iglo-OS installed the container, so it can update and remove it.
    managed: boolean
    // planned is set for an app that Iglo-OS can't install yet.
    planned: boolean
    stats?: AppStats
    version?: string
    updateAvailable: boolean
    hostUrl?: string
    // publicAddress is where people outside your network reach the app, like a playit.gg address.
    publicAddress?: string
    vmid?: number
    status?: string
    operation: AppOperation | null
    rollback?: string
    saved: SavedAnswers | null
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
        minecraft: MinecraftAnswers
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
    section:
        'selfUpdate' | 'notifications' | 'updates' | 'health' | 'backups' | 'tailscale' | 'jellyfin' | 'backgroundWork'
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
    // The demo build answers from sample data. Other builds leave this branch and the data out.
    if (import.meta.env.VITE_DEMO === '1') {
        const { demoRequest } = await import('@/demo/demo-api')
        return demoRequest<T>(method, path)
    }

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
    musicJobs: () => request<MusicQueue>('GET', '/music/jobs'),
    submitMusic: (input: { url: string; video: boolean }) => request<MusicJob>('POST', '/music/jobs', input),
    pauseMusic: (paused: boolean) => request<void>('PUT', '/music/pause', { paused }),
    photosRecovery: (action: 'backup' | 'verify') => request<void>('POST', `/photos/recovery/${action}`),
    installScopedApp: (id: string, scoped: ScopedAnswers) => request<void>('POST', `/apps/${id}/install`, { scoped }),
    gaming: () => request<Gaming>('GET', '/gaming'),
    playGaming: (play: boolean) => request<void>('PUT', '/gaming/play', { play }),
    endGamingSession: (id: string) => request<void>('POST', `/gaming/sessions/${encodeURIComponent(id)}/stop`),
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
    setJellyfinTheme: (enabled: boolean) => request<void>('PUT', '/jellyfin/theme', { enabled }),
    selfUpdate: () => request<SelfUpdate>('GET', '/self-update'),
    checkSelfUpdate: () => request<SelfUpdate>('POST', '/self-update/check'),
    installSelfUpdate: () => request<void>('POST', '/self-update/install'),
    saveSelfUpdateSettings: (settings: SelfUpdateSettings) => request<void>('PUT', '/self-update/settings', settings),
    backups: () => request<Backups>('GET', '/backups'),
    saveBackupJob: (job: BackupJob) => request<void>('PUT', '/backups/job', job),
    backUpGuest: (vmid: number) => request<void>('POST', `/backups/guests/${vmid}`),
    restoreBackup: (vmid: number, volid: string) => request<void>('POST', '/backups/restore', { vmid, volid }),
    deleteBackup: (volid: string) => request<void>('POST', '/backups/delete', { volid }),
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
    installMinecraft: (minecraft: MinecraftAnswers, keepSecrets = false) =>
        request<void>('POST', '/apps/minecraft/install', { minecraft, keepSecrets }),
    retryApp: (id: string) => request<void>('POST', `/apps/${id}/retry`),
    forgetAppAnswers: (id: string) => request<void>('DELETE', `/apps/${id}/answers`),
    updateApp: (id: string) => request<void>('POST', `/apps/${id}/update`),
    removeApp: (id: string) => request<void>('POST', `/apps/${id}/remove`),
    minecraftPlayers: () => request<MinecraftPlayers>('GET', '/apps/minecraft/players'),
    changeMinecraftPlayers: (players: MinecraftPlayers) => request<void>('PUT', '/apps/minecraft/players', players),
    vpnSettings: (id: string) => request<VpnView>('GET', `/apps/${id}/vpn`),
    changeVpn: (id: string, settings: VpnSettings) => request<void>('PUT', `/apps/${id}/vpn`, settings),
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
