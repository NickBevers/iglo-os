import type { QueryClient } from '@tanstack/react-query'

// Live updates: the manager says which topic changed over one Server-Sent
// Events stream, and the page asks for that topic at once. The polls stay as
// a fallback, slower while the stream is live.

let live = false

// isLive reports whether the stream is open. Poll intervals read it each time.
export function isLive() {
    return live
}

// topicKeys are the queries that each topic of the manager refreshes.
const topicKeys: Record<string, string[]> = {
    health: ['health'],
    updates: ['updates'],
    backups: ['backups'],
    apps: ['apps'],
    gaming: ['gaming'],
    workloads: ['workloads'],
    music: ['music-jobs'],
    inbox: ['inbox-jobs'],
    jellyfin: ['jellyfin'],
    setup: ['setup'],
}

// connectLive opens the stream and returns a function that closes it.
export function connectLive(client: QueryClient): () => void {
    if (import.meta.env?.VITE_DEMO === '1') {
        return () => {}
    }
    const source = new EventSource('/api/events')
    const pending = new Set<string>()
    let timer: ReturnType<typeof setTimeout> | undefined

    // A burst of events, like the lines of an install, refreshes each query once.
    const refresh = (topics: Iterable<string>) => {
        for (const topic of topics) {
            pending.add(topic)
        }
        if (timer) return
        timer = setTimeout(() => {
            timer = undefined
            for (const topic of pending) {
                const key = topicKeys[topic]
                if (key) {
                    client.invalidateQueries({ queryKey: key })
                }
            }
            pending.clear()
        }, 200)
    }

    // Invalidation also wakes active query observers so their interval
    // functions immediately see the new connection state.
    const refreshActive = () => {
        for (const queryKey of Object.values(topicKeys)) {
            void client.invalidateQueries({ queryKey, refetchType: 'active' })
        }
    }
    source.onopen = () => {
        live = true
        // Includes the initial connection: a change may have happened after
        // the page fetched its first snapshot but before the stream opened.
        refreshActive()
    }
    source.onerror = () => {
        if (live) {
            live = false
            refreshActive()
        }
    }
    source.onmessage = (event: MessageEvent<string>) => refresh([event.data])

    return () => {
        source.close()
        clearTimeout(timer)
        live = false
    }
}
