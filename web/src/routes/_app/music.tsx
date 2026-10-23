import { createFileRoute } from '@tanstack/react-router'
import { MusicPage } from '@/features/apps/music-page'
export const Route = createFileRoute('/_app/music')({ component: MusicPage })
