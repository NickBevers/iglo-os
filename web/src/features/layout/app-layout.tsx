import { Outlet, useRouterState } from '@tanstack/react-router'

import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { useReloadOnNewVersion } from '@/features/self-update/use-reload-on-new-version'
import { AppSidebar } from './app-sidebar'
import { navigation } from './navigation'
import { StatusBar } from './status-bar'

export function AppLayout() {
    const pathname = useRouterState({ select: (state) => state.location.pathname })
    const currentPage = navigation.flatMap((group) => group.items).find((item) => item.to === pathname)?.label
    useReloadOnNewVersion()

    return (
        <SidebarProvider>
            <AppSidebar />
            <SidebarInset>
                <StatusBar page={currentPage ?? (pathname === '/settings' ? 'Settings' : undefined)} />
                <div className="mx-auto w-full max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
                    <Outlet />
                </div>
            </SidebarInset>
        </SidebarProvider>
    )
}
