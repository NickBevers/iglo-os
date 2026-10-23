import { createFileRoute } from '@tanstack/react-router'
import { GamingPage } from '@/features/gaming/gaming-page'
export const Route = createFileRoute('/_app/gaming')({ component: GamingPage })
