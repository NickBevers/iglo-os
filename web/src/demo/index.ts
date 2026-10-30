// The demo build answers the API in the browser, so the web app runs as a
// static site without a manager. It is only loaded when VITE_DEMO is 1.
//
// fixtures.json holds the GET answers of the dev mock. Record it again with
// the manager on the mock, see docs/development.md. Times in it move along
// with the clock, so a backup from a day ago stays a day ago. The usage
// charts are made up here instead, because their points are most of the data.
// Anything that would change something fails with a message.
import fixtures from './fixtures.json'

const answers = fixtures.answers as Record<string, unknown>
const shift = Date.now() - Date.parse(fixtures.recordedAt)
const isoTime = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/
const readOnly = 'This is a demo with fake data. It can’t change anything.'

let signedIn = true

function json(status: number, body?: unknown) {
    if (body === undefined) {
        return new Response(null, { status })
    }
    return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

// moveTimes shifts every timestamp by the time since the recording.
function moveTimes(value: unknown): unknown {
    return JSON.parse(JSON.stringify(value), (_key, item) =>
        typeof item === 'string' && isoTime.test(item) ? new Date(Date.parse(item) + shift).toISOString() : item,
    )
}

// usage makes up the chart points of a node or a guest: 60 points that end now.
function usage(path: string, timeframe: string) {
    const steps: Record<string, number> = { hour: 60, day: 1800, week: 10800, month: 43200, year: 604800 }
    const step = steps[timeframe] ?? 60
    const vmid = Number(path.split('/')[4] ?? 0)
    const guest = (
        answers['/overview'] as { guests: { vmid: number; mem: number; maxMem: number; cpu: number }[] }
    ).guests.find((item) => item.vmid === vmid)
    const maxMem = guest ? guest.maxMem : 34359738368
    const mem = guest ? guest.mem : 10.5e9
    const cpu = guest ? guest.cpu : 0.1
    const now = Math.floor(Date.now() / 1000 / step) * step
    let seed = vmid + 1
    const random = () => {
        seed = (seed * 16807) % 2147483647
        return seed / 2147483647
    }
    const points = Array.from({ length: 60 }, (_, index) => {
        const wave = Math.sin((index / 60) * Math.PI * 4 + vmid)
        return {
            time: now - (59 - index) * step,
            cpu: Math.max(0, cpu * (1 + 0.5 * wave) + 0.03 * random()),
            mem: Math.min(maxMem, mem * (1 + 0.08 * wave + 0.02 * random())),
            maxMem,
            netIn: 2e5 + 3e5 * random(),
            netOut: 1e5 + 1.5e5 * random(),
        }
    })
    return { points }
}

function answer(method: string, url: URL, body: BodyInit | null | undefined) {
    const path = url.pathname.slice('/api'.length)
    const key = path + url.search

    if (path === '/auth/login' && method === 'POST') {
        signedIn = true
        const { username } = JSON.parse(String(body ?? '{}')) as { username?: string }
        return json(200, { username: username || 'admin' })
    }
    if (path === '/auth/logout' && method === 'POST') {
        signedIn = false
        return json(204)
    }
    if (!signedIn) {
        return json(401, { error: 'Sign in again.' })
    }
    if (path === '/health/refresh' && method === 'POST') {
        return json(200, moveTimes(answers['/health']))
    }
    if (method !== 'GET') {
        return json(403, { error: readOnly })
    }
    if (path.startsWith('/usage/')) {
        return json(200, usage(path, url.searchParams.get('timeframe') ?? 'hour'))
    }
    if (key in answers) {
        return json(200, moveTimes(answers[key]))
    }
    return json(404, { error: 'The demo has no data for this.' })
}

const realFetch = window.fetch.bind(window)

window.fetch = (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(input instanceof Request ? input.url : String(input), window.location.href)
    if (url.origin !== window.location.origin || !url.pathname.startsWith('/api/')) {
        return realFetch(input, init)
    }
    const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase()
    return Promise.resolve(answer(method, url, init?.body))
}

// The live stream never opens, so the pages keep to their polls.
class QuietEventSource extends EventTarget {
    readonly url: string
    readonly withCredentials = false
    readonly readyState = 0
    onopen: ((event: Event) => void) | null = null
    onmessage: ((event: MessageEvent) => void) | null = null
    onerror: ((event: Event) => void) | null = null

    constructor(url: string | URL) {
        super()
        this.url = String(url)
    }

    close() {}
}

window.EventSource = QuietEventSource as unknown as typeof EventSource

const notice = document.createElement('div')
notice.setAttribute('role', 'note')
notice.className =
    'pointer-events-none fixed right-4 bottom-4 z-50 rounded-full bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground shadow-md'
notice.textContent = 'Demo with fake data'
document.body.append(notice)
