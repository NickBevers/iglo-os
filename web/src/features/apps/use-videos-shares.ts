import { useQuery } from '@tanstack/react-query'

import { api, type Apps } from '@/lib/api'

// useVideosShares lists the mounted Videos shares from Storage. With write,
// only shares with write access count. preferred is the share that Jellyfin
// or the media stack already uses, so both apps see the same media, or the
// only share there is.
export function useVideosShares(defaults: Apps['defaults'], write: boolean) {
    const query = useQuery({ queryKey: ['storage-bindings'], queryFn: api.storageBindings })
    const shares = (query.data?.bindings ?? []).filter(
        (binding) => binding.role === 'videos' && binding.mounted && (!write || !binding.readOnly),
    )
    const users = [defaults.jellyfinVmid, defaults.mediaVmid].filter(Boolean).map(String)
    const used = shares.find((share) => share.consumers.some((consumer) => users.includes(consumer)))
    const preferred = used?.id ?? (shares.length === 1 ? shares[0].id : '')

    return { shares, preferred, error: query.error, loading: query.isPending }
}
