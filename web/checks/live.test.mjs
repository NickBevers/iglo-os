import assert from 'node:assert/strict'
import { test } from 'node:test'
import { QueryClient, QueryObserver } from '@tanstack/react-query'
import { connectLive, isLive } from '../src/lib/live.ts'

class Stream {
    static current
    constructor() { Stream.current = this }
    close() {}
}
globalThis.EventSource = Stream
const tick = () => new Promise((resolve) => setTimeout(resolve, 0))

test('connection transitions immediately refresh active snapshots and interval decisions', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const reads = []
    let requests = 0
    const observer = new QueryObserver(client, {
        queryKey: ['updates'],
        queryFn: async () => ++requests,
        refetchInterval: () => { reads.push(isLive()); return isLive() ? 60000 : 2000 },
    })
    const unsubscribe = observer.subscribe(() => {})
    const stop = connectLive(client)
    try {
        await tick()
        assert.equal(requests, 1)
        Stream.current.onopen()
        await tick()
        assert.equal(requests, 2, 'initial connection closes the first-snapshot gap')
        assert.equal(reads.at(-1), true)
        Stream.current.onerror()
        await tick()
        assert.equal(requests, 3)
        assert.equal(reads.at(-1), false, 'fallback is rescheduled without waiting a minute')
        Stream.current.onerror()
        await tick()
        assert.equal(requests, 3, 'failed reconnects do not produce request storms')
        Stream.current.onopen()
        await tick()
        assert.equal(requests, 4)
    } finally { stop(); unsubscribe(); client.clear() }
})

test('continuous event bursts cannot postpone refresh indefinitely', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    let requests = 0
    const observer = new QueryObserver(client, { queryKey: ['health'], queryFn: async () => ++requests })
    const unsubscribe = observer.subscribe(() => {})
    const stop = connectLive(client)
    let producer
    try {
        await tick()
        Stream.current.onopen()
        await tick()
        const before = requests
        producer = setInterval(() => Stream.current.onmessage({ data: 'health' }), 30)
        await new Promise((resolve) => setTimeout(resolve, 280))
        assert.ok(requests > before, 'a continuous stream must refresh within its throttle window')
        assert.ok(requests <= before + 2, 'bursts remain coalesced')
    } finally { clearInterval(producer); stop(); unsubscribe(); client.clear() }
})
