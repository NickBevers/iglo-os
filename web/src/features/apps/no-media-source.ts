export type MediaSource = {
    nasServer: string
    nasExport: string
    smbServer: string
    smbShare: string
    smbUsername: string
    smbPassword: string
    mediaFolder: string
}

// noMediaSource has no media answers, for media that is already mounted.
export const noMediaSource: MediaSource = {
    nasServer: '',
    nasExport: '',
    smbServer: '',
    smbShare: '',
    smbUsername: '',
    smbPassword: '',
    mediaFolder: '',
}
