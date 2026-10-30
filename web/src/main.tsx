import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRouter, RouterProvider } from '@tanstack/react-router'

import './index.css'
import { ThemeProvider } from '@/components/theme-provider'
import { TooltipProvider } from '@/components/ui/tooltip'
import { ApiError } from '@/lib/api'
import { sessionQuery } from '@/lib/queries'
import { routeTree } from './routeTree.gen'

// The demo build fakes the API in the browser. A normal build drops this.
if (import.meta.env.VITE_DEMO === '1') {
    await import('./demo')
}

function isClientError(error: unknown) {
    return error instanceof ApiError && error.status < 500
}

const queryClient = new QueryClient({
    queryCache: new QueryCache({
        onError(error, query) {
            // The session expired or the server restarted: go back to login,
            // and back to this page after it. The session query itself is
            // handled by the route guards.
            if (error instanceof ApiError && error.status === 401 && query.queryKey[0] !== sessionQuery.queryKey[0]) {
                queryClient.removeQueries({ queryKey: sessionQuery.queryKey })
                const { pathname, href } = router.state.location
                if (pathname !== '/login') {
                    router.navigate({ to: '/login', search: { redirect: href } })
                }
            }
        },
    }),
    defaultOptions: {
        queries: {
            retry: (failureCount, error) => !isClientError(error) && failureCount < 2,
        },
    },
})

const router = createRouter({
    routeTree,
    context: { queryClient },
    defaultPreload: 'intent',
    scrollRestoration: true,
})

declare module '@tanstack/react-router' {
    interface Register {
        router: typeof router
    }
}

createRoot(document.getElementById('root')!).render(
    <StrictMode>
        <ThemeProvider>
            <QueryClientProvider client={queryClient}>
                <TooltipProvider delay={300}>
                    <RouterProvider router={router} />
                </TooltipProvider>
            </QueryClientProvider>
        </ThemeProvider>
    </StrictMode>,
)
