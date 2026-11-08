import assert from 'node:assert/strict'
import { test } from 'node:test'
import { readFileSync } from 'node:fs'
import ts from 'typescript'
import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { api } from '../../lib/api.ts'
import { connectLive } from '../../lib/live.ts'
import {
    folderStats,
    gb,
    kindNames,
    maxShrinkFiles,
    selectionState,
    shrinkInterval,
    submissionPaths,
    toggleFiles,
} from './shrink-tree.ts'

const ready = {
    path: 'anime/Title/Season 01/one.mkv',
    bytes: 1e9,
    estimates: { standard: 270e6, high: 370e6 },
    status: 'ready',
}
const second = { ...ready, path: 'anime/Title/Season 01/two.mkv' }
const small = { ...ready, path: 'anime/Title/small.mkv', bytes: 310e6, status: 'small' }
const skipped = { ...ready, path: 'anime/Title/hdr.mkv', status: 'skipped', reason: 'HDR or Dolby Vision' }
const season = { path: 'anime/Title/Season 01', name: 'Season 01', folders: [], files: [ready, second] }
const title = { path: 'anime/Title', name: 'Title', folders: [season], files: [small, skipped] }
const kind = { path: 'anime', name: 'anime', folders: [title], files: [] }

test('parent selection uses ready files, mixed state and profile estimates', () => {
    const stats = folderStats(kind, 'standard')
    assert.equal(stats.ready.length, 2)
    assert.equal(stats.bytes, 3.31e9)
    assert.equal(stats.after, 1.85e9)
    assert.equal(folderStats(kind, 'high').after, 2.05e9)
    assert.deepEqual(selectionState(stats.ready, new Set([ready.path])), {
        checked: false,
        mixed: true,
        disabled: false,
    })
    const all = toggleFiles(stats.ready, new Set([ready.path]))
    assert.deepEqual([...all], [ready.path, second.path])
    assert.deepEqual([...toggleFiles(stats.ready, all)], [])
    assert.equal(selectionState([], all).disabled, true)
    assert.deepEqual(submissionPaths([kind], all), [ready.path, second.path])
    assert.deepEqual(submissionPaths([kind], new Set([ready.path])), [ready.path])
    assert.deepEqual(submissionPaths([kind], new Set([skipped.path])), [])
})
test('shrink fallback polls once a minute only while work is pending', () => {
    for (const state of ['queued', 'running']) assert.equal(shrinkInterval([{ state }]), 60000)
    assert.equal(shrinkInterval([{ state: 'done', rescanned: false }]), 60000)
    for (const state of ['failed', 'changed', 'not-smaller', 'cancelled'])
        assert.equal(shrinkInterval([{ state }]), false)
    assert.equal(shrinkInterval([{ state: 'done', rescanned: true }]), false)
    assert.equal(shrinkInterval([]), false)
    assert.equal(shrinkInterval(undefined), false)
})

const source = readFileSync(new URL('./shrink-library.tsx', import.meta.url), 'utf8')
const compiled = ts.transpileModule(source.replace(/^import[\s\S]*?from '[^']+'\n/gm, ''), {
    compilerOptions: { jsx: ts.JsxEmit.React, module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText
let state, hookIndex, buttons, mutation
let toasts = []
let jobs = []
let libraryData = { kinds: [kind], scannedAt: '2026-11-08T12:00:00Z' }
let libraryOptions
const useState = (initial) => {
    const i = hookIndex++
    if (!(i in state)) state[i] = initial
    return [
        state[i],
        (value) => {
            state[i] = typeof value === 'function' ? value(state[i]) : value
        },
    ]
}
const Button = ({ variant, ...props }) => {
    buttons.push(props)
    return React.createElement('button', props)
}
const Dialog = ({ open, children }) => (open ? React.createElement('div', { role: 'dialog' }, children) : null)
const exports = {}
const services = {
    React,
    useState,
    useEffect: React.useEffect,
    useMemo: (fn) => fn(),
    useRef: () => ({ current: null }),
    useQuery: (options) => {
        if (options.queryKey[0] === 'shrink-library') {
            libraryOptions = options
            return { data: libraryData }
        }
        return { data: { paused: true, jobs } }
    },
    useMutation: (options) => ({
        isPending: false,
        mutate: (fn) => {
            mutation = Promise.resolve()
                .then(fn)
                .then(options.onSuccess)
                .catch((error) => options.onError?.(error))
        },
    }),
    useQueryClient: () => ({ invalidateQueries: () => Promise.resolve() }),
    toast: { error: (message) => toasts.push(message) },
    api,
    Button,
    Card: 'div',
    CardContent: 'div',
    CardHeader: 'div',
    CardTitle: 'h3',
    Dialog,
    DialogContent: ({ finalFocus, ...props }) => React.createElement('div', props),
    DialogDescription: 'p',
    DialogFooter: 'div',
    DialogHeader: 'div',
    DialogTitle: 'h3',
    folderStats,
    gb,
    kindNames,
    maxShrinkFiles,
    selectionState,
    shrinkInterval,
    submissionPaths,
    toggleFiles,
}
new Function('exports', ...Object.keys(services), compiled)(exports, ...Object.values(services))
const render = () => {
    hookIndex = 0
    buttons = []
    return renderToStaticMarkup(React.createElement(exports.default))
}
const button = (label) => buttons.find((b) => b.children === label)

test('tree starts collapsed and expanded rows show skipped reasons and labelled controls', () => {
    state = []
    jobs = []
    const collapsed = render()
    assert.match(collapsed, /aria-expanded="false"/)
    assert.match(collapsed, /aria-label="Select Anime"/)
    assert.doesNotMatch(collapsed, /one\.mkv|HDR or Dolby Vision|Season 01/)
    assert.equal(button('Shrink…').disabled, true)
    state[6] = true
    state[7] = true
    state[8] = true
    const expanded = render()
    assert.match(expanded, /Select anime\/Title\/Season 01\/one.mkv/)
    assert.match(expanded, /already small/)
    assert.match(expanded, /skipped: HDR or Dolby Vision/)
    assert.match(expanded, /aria-label="Select anime\/Title\/hdr.mkv"[^>]*disabled/)
    jobs = [{ id: 'running', path: ready.path, state: 'running', progress: 0.4 }]
    assert.match(render(), /progress aria-label="Shrink progress for anime\/Title\/Season 01\/one.mkv"/)
})
test('submission requires the irreversible checkbox and uses the confirmed profile and numbers', async (t) => {
    state = [null, 'high', new Set([ready.path, second.path])]
    jobs = []
    const calls = []
    t.mock.method(globalThis, 'fetch', async (url, options) => {
        calls.push({ url, options })
        return Response.json(
            { jobs: [], refused: [{ path: 'anime/Title', title: 'Title', reason: 'size format' }] },
            { status: 202 },
        )
    })
    render()
    button('Shrink…').onClick()
    let html = render()
    assert.match(html, /2 files selected, 2 GB now, about 0.74 GB after/)
    assert.match(html, /The originals are replaced and can&#x27;t be restored./)
    assert.match(html, /I understand the originals are replaced/)
    assert.equal(button('Replace originals and shrink').disabled, true)
    button('Replace originals and shrink').onClick()
    assert.equal(calls.length, 0)
    state[4] = true
    state[1] = 'standard'
    render()
    assert.equal(button('Replace originals and shrink').disabled, false)
    button('Replace originals and shrink').onClick()
    await mutation
    assert.equal(calls.length, 1)
    assert.equal(calls[0].url, '/api/inbox/shrink')
    assert.equal(calls[0].options.headers['X-Iglo-Request'], '1')
    assert.deepEqual(JSON.parse(calls[0].options.body), {
        paths: [ready.path, second.path],
        profile: 'high',
        confirm: 'replace-originals',
    })
    html = render()
    assert.match(html, /Refused titles/)
    assert.match(html, /Title: size format/)
    assert.doesNotMatch(html, /role="dialog"/)
})
test('scan, queue and cancellation requests preserve methods and encode IDs', async (t) => {
    const calls = []
    t.mock.method(globalThis, 'fetch', async (url, options) => {
        calls.push({ url, options })
        return new Response(null, { status: 204 })
    })
    await api.scanLibrary()
    await api.shrinkSettings(false)
    await api.cancelShrink('a/b')
    assert.deepEqual(
        calls.map((c) => [c.options.method, c.url]),
        [
            ['POST', '/api/inbox/library/scan'],
            ['PUT', '/api/inbox/shrink/settings'],
            ['POST', '/api/inbox/shrink/cancel/a%2Fb'],
        ],
    )
    assert.deepEqual(JSON.parse(calls[1].options.body), { paused: false })
})

class Stream {
    static current
    constructor() {
        Stream.current = this
    }
    close() {}
}

test('library and shrink events invalidate their own queries', async () => {
    const client = {}
    const calls = []
    client.invalidateQueries = ({ queryKey }) => {
        calls.push(queryKey)
        return Promise.resolve()
    }
    globalThis.EventSource = Stream
    const stop = connectLive(client)
    try {
        Stream.current.onmessage({ data: 'library' })
        Stream.current.onmessage({ data: 'shrink' })
        await new Promise((resolve) => setTimeout(resolve, 240))
        assert.deepEqual(calls, [['shrink-library'], ['shrink-jobs']])
    } finally {
        stop()
    }
})

test('a requested scan keeps a fallback before the worker starts and stops after its snapshot changes', async (t) => {
    state = []
    jobs = []
    t.mock.method(globalThis, 'fetch', async () => new Response(null, { status: 204 }))
    render()
    button('Scan library').onClick()
    await mutation
    render()
    assert.equal(libraryOptions.refetchInterval({ state: { data: libraryData } }), 60000)
    assert.equal(button('Scanning…').disabled, true)
    libraryData = { ...libraryData, scannedAt: '2026-11-08T12:01:00Z' }
    render()
    assert.equal(libraryOptions.refetchInterval({ state: { data: libraryData } }), false)
    assert.equal(button('Scan library').disabled, false)
})

test('folder selections never submit a file discovered after selection', () => {
    const selected = toggleFiles(folderStats(kind, 'standard').ready, new Set())
    const later = {
        ...kind,
        folders: [
            {
                ...title,
                folders: [
                    { ...season, files: [ready, second, { ...ready, path: 'anime/Title/Season 01/unseen.mkv' }] },
                ],
            },
        ],
    }
    assert.deepEqual(submissionPaths([later], selected), [ready.path, second.path])
})

test('the page names the 100 file limit and blocks oversized selections before confirmation', () => {
    const previous = libraryData
    const many = Array.from({ length: 101 }, (_, index) => ({ ...ready, path: `anime/Large/${index}.mkv` }))
    libraryData = {
        kinds: [
            {
                path: 'anime',
                name: 'anime',
                folders: [{ path: 'anime/Large', name: 'Large', folders: [], files: many }],
                files: [],
            },
        ],
    }
    try {
        state = [null, 'standard', new Set(many.map((file) => file.path))]
        jobs = []
        let html = render()
        assert.match(html, /Select fewer files. You can shrink up to 100 files at a time./)
        assert.equal(button('Shrink…').disabled, true)
        state = [null, 'standard', new Set(many.slice(0, 100).map((file) => file.path))]
        html = render()
        assert.match(html, /Up to 100 files per submission/)
        assert.equal(button('Shrink…').disabled, false)
    } finally {
        libraryData = previous
    }
})

test('successful confirmed jobs also resume the queue', async (t) => {
    state = [null, 'standard', new Set([ready.path])]
    jobs = []
    toasts = []
    const calls = []
    t.mock.method(globalThis, 'fetch', async (url, options) => {
        calls.push({ url, options })
        return options.method === 'POST'
            ? Response.json({ jobs: [{ id: 'new', path: ready.path }], refused: [] }, { status: 202 })
            : new Response(null, { status: 204 })
    })
    render()
    button('Shrink…').onClick()
    state[4] = true
    render()
    button('Replace originals and shrink').onClick()
    await mutation
    assert.deepEqual(
        calls.map((call) => [call.options.method, call.url]),
        [
            ['POST', '/api/inbox/shrink'],
            ['PUT', '/api/inbox/shrink/settings'],
        ],
    )
    assert.deepEqual(JSON.parse(calls[0].options.body).paths, [ready.path])
    assert.deepEqual(JSON.parse(calls[1].options.body), { paused: false })
    assert.deepEqual(toasts, [])
    assert.doesNotMatch(render(), /role="dialog"/)
})

test('a resume failure reports queued jobs without submitting them again', async (t) => {
    state = [null, 'standard', new Set([ready.path])]
    jobs = []
    toasts = []
    const calls = []
    t.mock.method(globalThis, 'fetch', async (url, options) => {
        calls.push({ url, options })
        return options.method === 'POST'
            ? Response.json({ jobs: [{ id: 'new' }], refused: [] }, { status: 202 })
            : Response.json({ error: 'worker unavailable' }, { status: 502 })
    })
    render()
    button('Shrink…').onClick()
    state[4] = true
    render()
    button('Replace originals and shrink').onClick()
    await mutation
    assert.equal(calls.length, 2)
    assert.equal(calls.filter((call) => call.options.method === 'POST').length, 1)
    assert.match(toasts[0], /Jobs were added, but the queue could not resume: worker unavailable/)
    assert.doesNotMatch(render(), /role="dialog"/)
})

test('a full queue error is shown and never resumes work', async (t) => {
    state = [null, 'standard', new Set([ready.path])]
    jobs = []
    toasts = []
    const calls = []
    t.mock.method(globalThis, 'fetch', async (url, options) => {
        calls.push({ url, options })
        return Response.json(
            { error: 'The queue is full. Wait for jobs to finish or cancel queued jobs.' },
            { status: 400 },
        )
    })
    render()
    button('Shrink…').onClick()
    state[4] = true
    render()
    button('Replace originals and shrink').onClick()
    await mutation
    assert.equal(calls.length, 1)
    assert.match(toasts[0], /The queue is full/)
    assert.match(render(), /role="dialog"/)
})
