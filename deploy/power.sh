#!/usr/bin/env bash
# Lets the CPU clock down when it has nothing to do. iglo-power.service runs
# it at boot.
#
# The Proxmox kernel starts on the performance governor
# (CONFIG_CPU_FREQ_DEFAULT_GOV_PERFORMANCE in proxmox/pve-kernel,
# debian/rules.d/config-common.opts). With intel_pstate and HWP, that sets the
# lowest speed to the highest and the energy preference to 0, the most power
# hungry (intel_pstate_hwp_set in drivers/cpufreq/intel_pstate.c). The
# powersave governor lets the CPU pick its speed again, and it still boosts
# under load. Checked against Linux v7.0 on 28 October 2026.
#
# Only intel_pstate and amd-pstate-epp in active mode work like that. With
# other drivers, like acpi-cpufreq, powersave pins the lowest speed, so this
# leaves them alone.
set -euo pipefail

CPUFREQ="${CPUFREQ:-/sys/devices/system/cpu/cpufreq}"
# IGLO_EPP is the energy preference: balance_performance by default, or
# balance_power to save more. Set it with "systemctl edit iglo-power".
EPP="${IGLO_EPP:-balance_performance}"

case "$EPP" in
    default | performance | balance_performance | balance_power | power) ;;
    *)
        echo "unknown energy preference: $EPP" >&2
        exit 1
        ;;
esac

driver="$(cat "$CPUFREQ/policy0/scaling_driver" 2>/dev/null || true)"
case "$driver" in
    intel_pstate | amd-pstate-epp) ;;
    *)
        echo "CPU driver ${driver:-unknown}: left as it is"
        exit 0
        ;;
esac

preference="no energy preference"
for policy in "$CPUFREQ"/policy*; do
    # The governor first: intel_pstate refuses a new preference under performance.
    echo powersave >"$policy/scaling_governor"
    # The preference exists with HWP or amd-pstate-epp.
    if [[ -w "$policy/energy_performance_preference" ]]; then
        echo "$EPP" >"$policy/energy_performance_preference"
        preference="energy preference $EPP"
    fi
done
echo "CPU driver $driver: governor powersave, $preference"
