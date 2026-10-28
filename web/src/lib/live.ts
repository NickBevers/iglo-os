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
    if (import.meta.env.VITE_DEMO === '1') {
        return () => {}
    }
    const source = new EventSource('/api/events')
    const pending = new Set<string>()
    let timer: ReturnType<typeof setTimeout> | undefined
    let dropped = false

    // A burst of events, like the lines of an install, refreshes each query once.
    const refresh = (topics: Iterable<string>) => {
        for (const topic of topics) {
            pending.add(topic)
        }
        clearTimeout(timer)
        timer = setTimeout(() => {
            for (const topic of pending) {
                const key = topicKeys[topic]
                if (key) {
                    client.invalidateQueries({ queryKey: key })
                }
            }
            pending.clear()
        }, 200)
    }

    source.onopen = () => {
        live = true
        // Events may have gone missing while the stream was closed.
        if (dropped) {
            refresh(Object.keys(topicKeys))
        }
    }
    source.onerror = () => {
        live = false
        dropped = true
    }
    source.onmessage = (event: MessageEvent<string>) => refresh([event.data])

    return () => {
        source.close()
        clearTimeout(timer)
        live = false
    }
}
