# Settings file

On the Settings page (account menu), download all settings as `iglo.yaml`. Keep it in git, or import it on a new server.

```yaml
version: 1
notifications:
  ntfy: # no ntfy key keeps it as it is, an empty topic turns it off
    server: https://ntfy.sh
    topic: iglo-q8m2xv7kd0p4w1n9tz3c
updates:
  enabled: true
  day: sun
  time: "04:00"
  exclude: [105]
backups:
  enabled: true
  days: [] # no days means every day
  time: "03:00"
  storage: local
  exclude: []
  keep: { daily: 7, weekly: 4, monthly: 3 }
health:
  checks:
    - { name: Jellyfin, type: http, target: "http://192.168.1.20:8096" }
    - { name: SSH, type: tcp, target: "192.168.1.2:22" }
tailscale:
  serve: true
  shareSubnet: true
  subnet: 192.168.1.0/24
jellyfin:
  url: http://192.168.1.20:8096
backgroundWork:
  immich: { control: true, url: http://192.168.1.30:2283, node: pve }
  workers: true
  reserveGiB: 4
  maxQueues: 1
  queues: [videoConversion, faceDetection, facialRecognition, smartSearch]
  window: { enabled: true, start: "02:00", end: "07:00", timezone: Europe/Brussels }
```

- Tokens, passwords and API keys are not in the file. Enter the ntfy access token and the Jellyfin API key on their pages. An import skips the Jellyfin section until its key is saved.
- Background work leaves out the Immich API key, the gaming hold and which queues Iglo-OS paused. An import skips the section when it turns control on before an API key is saved, or when it names another Immich server. Connect that server on the Background work page first, so its key never goes to the wrong server.
- A section that is not in the file stays as it is.
- An import first shows which sections change. Nothing is saved until you apply it.
- Unknown keys are an error, so a typo does not go unnoticed.
