import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'

import { connectLive } from '@/lib/live'

// useLiveUpdates keeps the stream of the manager open while you are logged in.
export function useLiveUpdates() {
    const client = useQueryClient()
    useEffect(() => connectLive(client), [client])
}
