// The demo build answers the API from recorded sample data, so the interface
// runs without a server, for a review. Run scripts/record-demo.mjs to record
// the data again.
import { ApiError } from '@/lib/api'
import fixtures from './fixtures.json'

const answers = fixtures as Record<string, unknown>

export const demoMessage = 'This is a demo with sample data. Nothing changes here.'

// lookup finds the answer for a path: with its query, without it, and for the
// history of a guest, the history of the host in the same timeframe.
function lookup(path: string): unknown {
    const [pathname, query = ''] = path.split('?')
    if (path in answers) {
        return answers[path]
    }
    if (pathname.startsWith('/usage/')) {
        const timeframe = new URLSearchParams(query).get('timeframe') ?? 'hour'
        return answers[`/usage/pve?timeframe=${timeframe}`]
    }
    if (pathname.startsWith('/logs/tasks/')) {
        return Object.entries(answers).find(([key]) => key.startsWith('/logs/tasks/'))?.[1]
    }
    return answers[pathname]
}

export async function demoRequest<T>(method: string, path: string): Promise<T> {
    // A short wait, so loading states show like they do on a real server.
    await new Promise((resolve) => setTimeout(resolve, 120))

    if (method === 'GET') {
        const answer = lookup(path)
        if (answer === undefined) {
            throw new ApiError(404, `${path} is not in the demo`)
        }
        return structuredClone(answer) as T
    }
    // Signing in and out works, so the login page can be tried.
    if (path === '/auth/login' || path === '/auth/logout') {
        return undefined as T
    }

    throw new ApiError(403, demoMessage)
}
