# Movies and series on the Videos share

Jellyfin and the media stack keep your movies and series on a **Videos** share of your NAS, like the other apps use their own shares. Connect it on the Storage page first. See [scoped-storage.md](scoped-storage.md).

1. On the NAS, share one folder for the videos, like `Videos`. Share that folder, not your whole home folder. Then the Proxmox host can't touch your other files.
   - On an Umbrel: open **Settings > File sharing**, or right-click the folder in Files and choose **Share over network**. That page also shows the username and password. They are not your Umbrel login.
2. Give the NAS a fixed IP in your router (a DHCP reservation).
3. In Iglo-OS, open **Server > Storage** and connect the share with the role **Videos**. Use the IP address of the NAS: the Proxmox host can't look up names like `umbrel.local`. Give it write access, because the media stack writes movies and series to it.

The install forms of Jellyfin and the media stack list the Videos shares. When one app already uses a share, the other app's form chooses that one, so both see the same media. Inbox also writes its converted home videos to a Videos share.

## Folders

Movies and series each get their own folder on the share. The forms ask for both names:

| Folder | Default  | On the NAS              | In the apps and Jellyfin |
| ------ | -------- | ----------------------- | ------------------------ |
| Movies | `movies` | `Videos/movies`         | `/data/media/movies`     |
| Series | `series` | `Videos/series`         | `/data/media/series`     |

- If a folder does not exist, the installer makes it. On a share without write access, make the folders on the NAS first.
- The installers never change folders or files that exist.

## Owners

SMB has no owners like Linux. The mount gives every file to group 101000 on the host, which is group 1000 in the containers, with write access for that group. The apps of the media stack run as user 1000, so they can write. Jellyfin reads everything and writes nothing: its container gets the share read-only.

If the media stack can't write to a folder, the NAS refuses the SMB user. Give that user write access to the share on the NAS, then click **Try again**.

## Good to know

- Proxmox does not back up the share with the containers: a backup of a container skips its shares. Back up your media in another way, for example on the NAS.
- An install that fails removes its container. The movies and series folders that it made stay.
- Removing an app keeps the share and the media. Disconnect the share on the Storage page once no app uses it.
- NFS shares and folders on a disk of the host are not supported for media.
