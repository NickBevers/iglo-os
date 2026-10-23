// Records the answers of the dev API into src/demo/fixtures.json, for the demo
// build. Run it from the web folder while `make dev` runs:
//
//	node scripts/record-demo.mjs
//
// Everything comes from the dev mock, so the fixtures hold sample data only.
import { execFileSync } from 'node:child_process'
import { writeFileSync } from 'node:fs'

const api = 'http://127.0.0.1:8080/api'
const code = execFileSync('go', ['run', './dev/mock', 'code'], { cwd: '..', encoding: 'utf8' }).trim()

const login = await fetch(`${api}/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Iglo-Request': '1' },
    body: JSON.stringify({ username: 'admin', password: 'iglo-dev-password', code }),
})
if (!login.ok) {
    throw new Error(`login failed: ${login.status}, wait for the next code and try again`)
}
const cookie = login.headers.get('set-cookie').split(';')[0]

async function get(path) {
    const response = await fetch(api + path, { headers: { cookie } })
    if (!response.ok) {
        throw new Error(`${path}: ${response.status} ${await response.text()}`)
    }
    return response.json()
}

// The keys are the paths the web app asks for. The demo looks up the path
// with its query first, then the path alone.
const fixtures = {}
for (const path of [
    '/auth/me',
    '/overview',
    '/workloads',
    '/updates',
    '/jellyfin',
    '/self-update',
    '/backups',
    '/tailscale',
    '/apps',
    '/apps/minecraft/players',
    '/apps/media/vpn',
    '/health',
    '/logs/tasks',
]) {
    fixtures[path] = await get(path)
}
for (const timeframe of ['hour', 'day', 'week', 'month', 'year']) {
    fixtures[`/usage/pve?timeframe=${timeframe}`] = await get(`/usage/pve?timeframe=${timeframe}`)
}
fixtures['/logs/journal'] = await get('/logs/journal?vmid=0&priority=6&lines=500')
fixtures['/logs/docker'] = await get('/logs/docker?vmid=130&lines=200')
const task = fixtures['/logs/tasks'][0]
if (task) {
    fixtures[`/logs/tasks/${task.node}/log`] = await get(
        `/logs/tasks/${task.node}/log?${new URLSearchParams({ upid: task.upid })}`,
    )
}

writeFileSync(new URL('../src/demo/fixtures.json', import.meta.url), JSON.stringify(fixtures, null, 2) + '\n')
console.log(`recorded ${Object.keys(fixtures).length} answers`)
