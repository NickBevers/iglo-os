import { createFileRoute } from '@tanstack/react-router'
import { StoragePage } from '@/features/apps/storage-page'
import { api } from '@/lib/api'
export const Route = createFileRoute('/_app/storage')({
    loader: ({ context }) => context.queryClient.prefetchQuery({ queryKey: ['storage-bindings'], queryFn: api.storageBindings }),
    component: StoragePage,
})
