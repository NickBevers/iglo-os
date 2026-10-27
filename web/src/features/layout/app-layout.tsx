import { Outlet, useRouterState } from '@tanstack/react-router'

import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { useReloadOnNewVersion } from '@/features/self-update/use-reload-on-new-version'
import { AppSidebar } from './app-sidebar'
import { DemoBanner } from './demo-banner'
import { navigation } from './navigation'
import { StatusBar } from './status-bar'
import { useLiveUpdates } from './use-live-updates'

// pageNames names the pages that are not in the sidebar navigation.
function pageNames(pathname: string) {
    if (pathname === '/settings') {
        return 'Settings'
    }
    return pathname.startsWith('/console/') ? 'Console' : undefined
}

export function AppLayout() {
    const pathname = useRouterState({ select: (state) => state.location.pathname })
    const currentPage = navigation.flatMap((group) => group.items).find((item) => item.to === pathname)?.label
    useReloadOnNewVersion()
    useLiveUpdates()

    return (
        <SidebarProvider>
            <AppSidebar />
            <SidebarInset>
                <DemoBanner />
                <StatusBar page={currentPage ?? pageNames(pathname)} />
                <div className="mx-auto w-full max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
                    <Outlet />
                </div>
            </SidebarInset>
        </SidebarProvider>
    )
}
