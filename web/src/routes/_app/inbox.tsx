import { createFileRoute } from '@tanstack/react-router'
import { InboxPage } from '@/features/apps/inbox-page'
export const Route = createFileRoute('/_app/inbox')({ component: InboxPage })
