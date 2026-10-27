import { createFileRoute } from '@tanstack/react-router'
import { SetupPage } from '@/features/setup/setup-page'
export const Route = createFileRoute('/_app/setup')({ component: SetupPage })
