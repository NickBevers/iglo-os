import assert from 'node:assert/strict'
import { test } from 'node:test'
import { readFileSync } from 'node:fs'
import ts from 'typescript'
import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { api, ApiError, subtitleLanguageButtons, toggleSubtitleLanguage } from '../src/lib/api.ts'

test('subtitle choices add and remove without duplicates', () => {
    const initial = ['en', 'nl']
    const added = toggleSubtitleLanguage(initial, 'fr')
    assert.deepEqual(added, ['en', 'nl', 'fr'])
    assert.deepEqual(initial, ['en', 'nl'])
    const removed = toggleSubtitleLanguage(added, 'nl')
    assert.deepEqual(removed, ['en', 'fr'])
    assert.deepEqual(toggleSubtitleLanguage(removed, 'nl'), ['en', 'fr', 'nl'])
})

test('ten subtitles is the limit and selected languages can still be removed', () => {
    const full = ['en', 'nl', 'fr', 'de', 'es', 'ja', 'ko', 'zh', 'pt', 'ar']
    assert.deepEqual(toggleSubtitleLanguage(full, 'it'), full)
    const removed = toggleSubtitleLanguage(full, 'fr')
    assert.deepEqual(toggleSubtitleLanguage(removed, 'it'), [
        'en',
        'nl',
        'de',
        'es',
        'ja',
        'ko',
        'zh',
        'pt',
        'ar',
        'it',
    ])
})

test('None clears all subtitles and a language can be picked afterward', () => {
    const initial = ['en', 'nl']
    assert.deepEqual(toggleSubtitleLanguage(initial, null), [])
    assert.deepEqual(initial, ['en', 'nl'])
    assert.deepEqual(toggleSubtitleLanguage([], null), [])
    assert.deepEqual(toggleSubtitleLanguage([], 'ja'), ['ja'])
    assert.deepEqual(toggleSubtitleLanguage(['ja'], 'ja'), [])
})

test('media settings requests use the API contract and keep server errors', async (t) => {
    const settings = {
        anime: { subtitles: [], audio: 'original' },
        dutch: { subtitles: [], audio: 'dutch' },
        series: { subtitles: ['nl', 'en'], audio: 'original' },
        films: { subtitles: ['nl'], audio: 'original' },
    }
    const calls = []
    t.mock.method(globalThis, 'fetch', async (url, options) => {
        calls.push({ url, options })
        return options.method === 'GET' ? Response.json({ settings }) : new Response(null, { status: 202 })
    })
    assert.deepEqual(await api.mediaSettings('media'), { settings })
    await api.changeMediaSettings('media', settings)
    assert.equal(calls[0].url, '/api/apps/media/media-settings')
    assert.equal(calls[0].options.method, 'GET')
    assert.equal(calls[0].options.body, undefined)
    assert.equal(calls[1].url, '/api/apps/media/media-settings')
    assert.equal(calls[1].options.method, 'PUT')
    assert.deepEqual(JSON.parse(calls[1].options.body), { settings })
    assert.equal(calls[1].options.headers['X-Iglo-Request'], '1')

    globalThis.fetch = async () => Response.json({ error: 'Update the media stack first.' }, { status: 409 })
    await assert.rejects(
        api.mediaSettings('media'),
        (error) =>
            error instanceof ApiError && error.status === 409 && error.message === 'Update the media stack first.',
    )
})

// Render the actual dialog components with app services replaced by local stubs.
// This keeps the checks independent of a running manager and its credentials.

const dialogSource = readFileSync(new URL('../src/features/apps/media-settings-dialog.tsx', import.meta.url), 'utf8')
const compiled = ts.transpileModule(
    dialogSource.replace(/^import[\s\S]*?from '[^']+'\n/gm, '') + '\nexport { MediaSettingsForm, MediaSettingsLoader }',
    { compilerOptions: { jsx: ts.JsxEmit.React, module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } },
).outputText
let queryError = null
const components = { exports: {} }
const Button = ({ variant, size, ...props }) => {
    renderedButtons.push(props)
    return React.createElement('button', props)
}
let hookState = null
let hookIndex = 0
let renderedButtons = []
let renderedSelects = []
const formReact = {
    ...React,
    createElement(tag, props, ...children) {
        if (tag === 'select') renderedSelects.push(props)
        return React.createElement(tag, props, ...children)
    },
}
function formUseState(initial) {
    if (hookState === null) return React.useState(initial)
    const index = hookIndex++
    if (!(index in hookState)) hookState[index] = initial
    return [
        hookState[index],
        (update) => {
            hookState[index] = typeof update === 'function' ? update(hookState[index]) : update
        },
    ]
}
new Function(
    'exports',
    'React',
    'useId',
    'useState',
    'useQuery',
    'useMutation',
    'useQueryClient',
    'Button',
    'appsQuery',
    'mediaSettingsQuery',
    'toggleSubtitleLanguage',
    'subtitleLanguageButtons',
    'api',
    'toast',
    'DialogFooter',
    compiled,
)(
    components.exports,
    formReact,
    React.useId,
    formUseState,
    () => ({ error: queryError }),
    () => ({ isPending: false }),
    () => ({}),
    Button,
    {},
    () => ({}),
    toggleSubtitleLanguage,
    subtitleLanguageButtons,
    {},
    {},
    'div',
)
const formMarkup = (selected, sizes = { series: 0, films: 0, films4k: 0 }) =>
    renderToStaticMarkup(
        React.createElement(components.exports.MediaSettingsForm, {
            app: { id: 'media' },
            onDone() {},
            current: {
                sizes,
                anime: { subtitles: selected, audio: 'original' },
                dutch: { subtitles: [], audio: 'dutch' },
                series: { subtitles: selected, audio: 'original' },
                films: { subtitles: selected, audio: 'original' },
            },
        }),
    )
const firstButtons = (html) => html.match(/<fieldset[\s\S]*?<\/fieldset>/)[0].match(/<button[\s\S]*?<\/button>/g)
const buttonLabels = (buttons) =>
    buttons.map((button) => button.replace(/<span[\s\S]*?<\/button>/, '').replace(/<[^>]+>/g, ''))

test('language buttons keep their order and mark picked languages', () => {
    const selected = ['ja', 'en']
    const first = firstButtons(formMarkup(selected))
    const next = firstButtons(formMarkup(toggleSubtitleLanguage(selected, 'nl')))
    const expected = [
        'None',
        'English',
        'Dutch',
        'French',
        'German',
        'Spanish',
        'Italian',
        'Portuguese',
        'Japanese',
        'Korean',
        'Chinese',
    ]
    assert.deepEqual(buttonLabels(first), expected)
    assert.deepEqual(buttonLabels(next), expected)
    assert.match(first[8], /aria-pressed="true"/)
    assert.match(first[1], /aria-pressed="true"/)
    assert.match(first[2], /aria-pressed="false"/)
    assert.match(next[2], /aria-pressed="true"/)
    assert.doesNotMatch(first.join(''), /Preference|<span/)
    assert.doesNotMatch(formMarkup(selected), /first is preferred|change the order/)
})

test('fieldset owns the help description and ordinary subtitle copy describes downloading', () => {
    const html = formMarkup(['en'])
    assert.equal((html.match(/<fieldset[^>]*aria-describedby=/g) ?? []).length, 4)
    assert.doesNotMatch(html, /<button[^>]*aria-describedby=/)
    const help = 'Bazarr downloads them for every title of this kind. Your player decides when to show them.'
    assert.equal(html.split(help).length - 1, 2)
})

test('the loader shows each server error without adding Update advice', () => {
    for (const [status, message] of [
        [409, 'the media stack needs an update first: update it on the Apps page, then change its languages'],
        [503, 'Bazarr is not answering yet. Try again in a minute.'],
        [409, 'another operation is running'],
        [409, 'the media stack is not running'],
    ]) {
        queryError = new ApiError(status, message)
        const html = renderToStaticMarkup(
            React.createElement(components.exports.MediaSettingsLoader, { app: { id: 'media' }, onDone() {} }),
        )
        assert.match(html, /role="alert"/)
        assert.equal(html.replace(/<[^>]+>/g, ''), message)
    }
    queryError = null
})

test('selected languages outside the fixed buttons are named and removable', () => {
    const selected = ['nl', 'pl', 'pb', 'zt', 'ea', 'qz', 'pb']
    const buttons = subtitleLanguageButtons(selected)
    assert.equal(buttons.length, 15)
    assert.deepEqual(buttons.slice(10), [
        { code: 'pl', name: 'Polish' },
        { code: 'pb', name: 'Portuguese (Brazil)' },
        { code: 'zt', name: 'Chinese (Traditional)' },
        { code: 'ea', name: 'Spanish (Latin America)' },
        { code: 'qz', name: 'qz' },
    ])
    assert.deepEqual(selected, ['nl', 'pl', 'pb', 'zt', 'ea', 'qz', 'pb'])
    const markup = firstButtons(formMarkup(['en', 'pl', 'pb']))
    assert.match(markup[11], /aria-pressed="true"[^>]*>Polish/)
    assert.match(markup[12], /aria-pressed="true"[^>]*>Portuguese \(Brazil\)/)
    assert.equal(subtitleLanguageButtons(toggleSubtitleLanguage(['en', 'pl'], 'pl')).length, 10)
})

test('extra languages stay visible after removing and None until the form closes', () => {
    hookState = []
    const render = () => {
        hookIndex = 0
        renderedButtons = []
        return firstButtons(formMarkup(['en', 'pl', 'pb']))
    }
    const click = (label) =>
        renderedButtons.find((button) => React.Children.toArray(button.children).includes(label)).onClick()
    try {
        const opened = buttonLabels(render())
        click('Polish')
        let buttons = render()
        assert.deepEqual(buttonLabels(buttons), opened)
        assert.match(buttons[11], /aria-pressed="false"/)
        click('Polish')
        buttons = render()
        assert.match(buttons[11], /aria-pressed="true"/)
        click('None')
        buttons = render()
        assert.deepEqual(buttonLabels(buttons), opened)
        assert.match(buttons[11], /aria-pressed="false"/)
        assert.match(buttons[12], /aria-pressed="false"/)
        click('Portuguese (Brazil)')
        assert.match(render()[12], /aria-pressed="true"/)
    } finally {
        hookState = null
    }
    assert.equal(firstButtons(formMarkup(['en'])).length, 11)
})

test('required subtitle choices keep their last language', () => {
    for (const language of ['en', 'pl', 'pb']) {
        assert.deepEqual(toggleSubtitleLanguage([language], language, false), [language])
        assert.deepEqual(toggleSubtitleLanguage([language], null, false), [language])
        assert.deepEqual(toggleSubtitleLanguage([language, 'fr'], language, false), ['fr'])
    }
})

test('only Anime and Dutch offer None and Series and Films keep the last button pressed', () => {
    hookState = []
    const render = () => {
        hookIndex = 0
        renderedButtons = []
        return formMarkup(['en'])
    }
    try {
        const fields = render().match(/<fieldset[\s\S]*?<\/fieldset>/g)
        assert.equal(fields.length, 4)
        for (const [index, field] of fields.entries()) {
            const buttons = field.match(/<button[\s\S]*?<\/button>/g)
            const labels = buttonLabels(buttons)
            assert.equal(labels.includes('None'), index < 2)
            if (index >= 2) {
                assert.match(field, /Series and films need at least one language\./)
                const english = renderedButtons.filter((button) => button.children === 'English')[index]
                assert.equal(english.disabled, false)
                english.onClick()
                const next = render().match(/<fieldset[\s\S]*?<\/fieldset>/g)[index]
                assert.match(next, /aria-pressed="true"[^>]*>English<\/button>/)
            }
        }
        const none = renderedButtons.filter((button) => button.children === 'None')
        assert.equal(none.length, 2)
        for (const button of none) button.onClick()
        const next = render().match(/<fieldset[\s\S]*?<\/fieldset>/g)
        for (const field of next.slice(0, 2)) assert.match(field, /aria-pressed="true"[^>]*>None<\/button>/)
    } finally {
        hookState = null
    }
})

test('saved custom download sizes stay available after choosing another size', () => {
    hookState = []
    const render = () => {
        hookIndex = 0
        renderedSelects = []
        return formMarkup(['en'], { series: 42, films: 67, films4k: 209 })
    }
    try {
        render()
        renderedSelects[0].onChange({ target: { value: '41' } })
        renderedSelects[1].onChange({ target: { value: '33' } })
        renderedSelects[2].onChange({ target: { value: '208' } })
        const html = render()
        assert.match(html, /value="42">Custom \(42 MB per minute\)/)
        assert.match(html, /value="67">Custom \(67 MB per minute\)/)
        assert.match(html, /value="209">Custom \(209 MB per minute\)/)
        renderedSelects[0].onChange({ target: { value: '42' } })
        render()
        renderedSelects[1].onChange({ target: { value: '67' } })
        renderedSelects[2].onChange({ target: { value: '209' } })
        const restored = render()
        assert.match(restored, /value="42" selected="">Custom \(42 MB per minute\)/)
        assert.match(restored, /value="67" selected="">Custom \(67 MB per minute\)/)
        assert.match(restored, /value="209" selected="">Custom \(209 MB per minute\)/)
    } finally {
        hookState = null
    }
})

test('4K presets use per-film sizes, retain other limits and enable Save', () => {
    hookState = []
    const render = () => {
        hookIndex = 0
        renderedSelects = []
        renderedButtons = []
        return formMarkup(['en'])
    }
    try {
        const html = render()
        assert.equal(renderedSelects.length, 3)
        assert.ok(html.includes('Films in HD (1080p and lower), per 2-hour film'))
        assert.ok(html.includes('Films in 4K, per 2-hour film'))
        assert.ok(html.includes('4K films are only downloaded when the media settings allow 4K.'))
        const select = html.match(/<select[\s\S]*?<\/select>/g)[2]
        assert.deepEqual(
            [...select.matchAll(/value="(\d+)"/g)].map((match) => Number(match[1])),
            [0, 333, 208, 166, 125],
        )
        assert.match(select, /aria-describedby="[^"]*-sizes-4k-help/)
        assert.equal(renderedButtons.find((button) => button.type === 'submit').disabled, true)
        for (const limit of ['333', '208', '166', '125', '0']) {
            renderedSelects[2].onChange({ target: { value: limit } })
            render()
            assert.deepEqual(hookState[0].sizes, { series: 0, films: 0, films4k: Number(limit) })
            assert.equal(renderedButtons.find((button) => button.type === 'submit').disabled, limit === '0')
        }
    } finally {
        hookState = null
    }
})

test('older settings without a 4K size show No limit and preserve HD size', () => {
    hookState = []
    try {
        hookIndex = 0
        renderedSelects = []
        formMarkup(['en'], { series: 41, films: 33 })
        assert.deepEqual(
            renderedSelects.map((select) => select.value),
            [41, 33, 0],
        )
        renderedSelects[2].onChange({ target: { value: '208' } })
        assert.deepEqual(hookState[0].sizes, { series: 41, films: 33, films4k: 208 })
    } finally {
        hookState = null
    }
})
