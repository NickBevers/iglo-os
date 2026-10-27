#!/usr/bin/env bash
# Gives Dolphin the Wii Remotes of a DolphinBar in mode 4, or of the host's
# Bluetooth. Wolf only adds its own virtual devices to the emulator container,
# so this init script adds the hidraw devices of Wii Remotes, and nothing else.
# The image's entrypoint sources it as root before the session starts, so it
# never exits and never fails the start.
gow_log "**** Iglo-OS: Wii Remotes ****"
for iglo_hidraw in /sys/class/hidraw/hidraw*; do
    [[ -r "$iglo_hidraw/device/uevent" && -r "$iglo_hidraw/dev" ]] || continue
    # HID_ID is bus:vendor:product. 0306 is a Wii Remote, 0330 a Wii Remote Plus.
    case "$(grep '^HID_ID=' "$iglo_hidraw/device/uevent" | tr 'a-f' 'A-F')" in
        *:0000057E:00000306 | *:0000057E:00000330) ;;
        *) continue ;;
    esac
    iglo_node="/dev/$(basename "$iglo_hidraw")"
    IFS=: read -r iglo_major iglo_minor <"$iglo_hidraw/dev" || continue
    if [[ ! -e "$iglo_node" ]] && ! mknod "$iglo_node" c "$iglo_major" "$iglo_minor"; then
        gow_log "Could not add $iglo_node"
        continue
    fi
    chmod 0666 "$iglo_node"
    gow_log "Added Wii Remote $iglo_node"
done
unset iglo_hidraw iglo_node iglo_major iglo_minor
