#!/usr/bin/env bash
# Secrets of an app container, like a VPN login. The host seals them with
# systemd-creds: with the TPM2 chip and the host key when the host has a
# TPM2, otherwise with the host key (--with-key=auto, systemd v257). They
# never lie on the disk of the container, so its backups don't have them.
# When the container starts, iglo-secrets@<id>.service unseals them into
# RAM, and the container reads them there.
# Checked against man/systemd-creds.xml of systemd v257 on 27 October 2026.

SECRETS_ROOT=/etc/iglo/credentials
SECRETS_RUN=/run/iglo/secrets
# Where the container sees them. Not under /run: the systemd of the container
# mounts its own /run over it at boot.
SECRETS_MOUNT=/mnt/iglo-secrets
# Root of an unprivileged container, as the host sees it.
SECRETS_OWNER=100000

# A secret is "run" when the container gets it, or "keep" when only the next
# change needs it, like the PIA password that Gluetun gets in another form.
secret_file() {
    printf '%s/%s/%s/%s.cred' "$SECRETS_ROOT" "$1" "$2" "$3"
}

secret_name_ok() {
    [[ "$1" =~ ^[0-9]{3,9}$ && ("$2" == run || "$2" == keep) && "$3" =~ ^[a-z0-9_]{1,64}$ ]]
}

# secret_put seals stdin as secret $3 of kind $2 of container $1. Empty input
# removes the secret. The value goes through a pipe, never a file or argument.
secret_put() {
    local ctid="$1" kind="$2" name="$3" value file
    secret_name_ok "$ctid" "$kind" "$name" || return 1
    value="$(cat)"
    file="$(secret_file "$ctid" "$kind" "$name")"
    install -d -m 0700 "$SECRETS_ROOT" "$SECRETS_ROOT/$ctid" "$(dirname "$file")"
    if [[ -z "$value" ]]; then
        rm -f "$file"
        return 0
    fi
    printf '%s' "$value" | systemd-creds encrypt --name="$name" - "$file.new"
    mv -f "$file.new" "$file"
}

# secret_get prints secret $3 of kind $2 of container $1, or nothing.
secret_get() {
    local file
    secret_name_ok "$1" "$2" "$3" || return 1
    file="$(secret_file "$1" "$2" "$3")"
    [[ -f "$file" ]] || return 0
    systemd-creds decrypt --name="$3" "$file" -
}

# secrets_unlock unseals the run secrets of container $1 into RAM, readable
# by the root of the container only. It removes the ones that are gone.
secrets_unlock() {
    local ctid="$1" dir="$SECRETS_RUN/$1" file name
    [[ "$ctid" =~ ^[0-9]{3,9}$ ]] || return 1
    install -d -m 0755 "$SECRETS_RUN"
    install -d -m 0500 -o "$SECRETS_OWNER" -g "$SECRETS_OWNER" "$dir"
    for file in "$SECRETS_ROOT/$ctid/run"/*.cred; do
        [[ -f "$file" ]] || continue
        name="$(basename "$file" .cred)"
        systemd-creds decrypt --name="$name" "$file" "$dir/.$name.new"
        chown "$SECRETS_OWNER:$SECRETS_OWNER" "$dir/.$name.new"
        chmod 0400 "$dir/.$name.new"
        mv -f "$dir/.$name.new" "$dir/$name"
    done
    for file in "$dir"/*; do
        [[ -f "$file" ]] || continue
        name="$(basename "$file")"
        [[ -f "$SECRETS_ROOT/$ctid/run/$name.cred" ]] || rm -f "$file"
    done
}

# secrets_attached says whether container $1 has its secrets folder.
secrets_attached() {
    pct config "$1" | grep -qE "^mp[0-9]+: $SECRETS_RUN/$1,"
}

# secrets_attach gives container $1 its secrets folder as mount point $2, and
# makes Proxmox unseal the secrets before the container starts. It takes
# effect at the next start of the container.
secrets_attach() {
    local ctid="$1" index="$2"
    secrets_unlock "$ctid"
    pct set "$ctid" "--mp$index" "$SECRETS_RUN/$ctid,mp=$SECRETS_MOUNT,ro=1,backup=0"
    install -d -m 0755 "/etc/systemd/system/pve-container@$ctid.service.d"
    printf '[Unit]\nRequires=iglo-secrets@%s.service\nAfter=iglo-secrets@%s.service\n' "$ctid" "$ctid" \
        >"/etc/systemd/system/pve-container@$ctid.service.d/iglo-secrets.conf"
    systemctl daemon-reload
}

# secrets_save keeps a copy of the sealed secrets of container $1, for
# secrets_restore when a change does not work.
secrets_save() {
    rm -rf "${SECRETS_ROOT:?}/$1.before"
    if [[ -d "$SECRETS_ROOT/$1" ]]; then
        cp -a "$SECRETS_ROOT/$1" "$SECRETS_ROOT/$1.before"
    else
        install -d -m 0700 "$SECRETS_ROOT/$1.before"
    fi
}

secrets_restore() {
    [[ -d "$SECRETS_ROOT/$1.before" ]] || return 0
    rm -rf "${SECRETS_ROOT:?}/$1"
    mv "$SECRETS_ROOT/$1.before" "$SECRETS_ROOT/$1"
    secrets_unlock "$1"
}

secrets_forget_saved() {
    rm -rf "${SECRETS_ROOT:?}/$1.before"
}

# secrets_remove deletes all secrets of container $1, sealed and unsealed.
secrets_remove() {
    [[ "$1" =~ ^[0-9]{3,9}$ ]] || return 1
    rm -rf "${SECRETS_ROOT:?}/$1" "${SECRETS_ROOT:?}/$1.before" "${SECRETS_RUN:?}/$1"
    rm -f "/etc/systemd/system/pve-container@$1.service.d/iglo-secrets.conf"
    systemctl daemon-reload
}
