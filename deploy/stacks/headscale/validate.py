#!/usr/bin/env python3
"""Check native controller answers, host caps and the static bridge address."""
import ipaddress
import json
import re
import sys


def fail(message):
    raise ValueError(message)


def answers(path):
    values = {}
    allowed = {'HEADSCALE_HOSTNAME', 'HEADSCALE_ADDRESS', 'HEADSCALE_GATEWAY', 'STORAGE', 'RATE_MB', 'PREREQUISITES'}
    with open(path, encoding='utf-8') as stream:
        for line in stream:
            key, sep, value = line.rstrip('\n').partition('=')
            if not sep or key not in allowed or key in values:
                fail('Unknown or repeated controller answer')
            values[key] = value
    host = values.get('HEADSCALE_HOSTNAME', '')
    labels = host.split('.')
    if (len(host) > 240 or len(labels) < 2 or
            any(not re.fullmatch(r'[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?', label) for label in labels) or
            host.endswith(('.local', '.localhost', '.internal', '.test', '.invalid', '.example'))):
        fail('Give a public lowercase DNS hostname without a scheme or port')
    try:
        ipaddress.ip_address(host)
    except ValueError:
        pass
    else:
        fail('Use a DNS hostname, not an IP address')
    address = ipaddress.IPv4Interface(values.get('HEADSCALE_ADDRESS', ''))
    gateway = ipaddress.IPv4Address(values.get('HEADSCALE_GATEWAY', ''))
    private = [ipaddress.ip_network(p) for p in ('10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16')]
    if (not any(address.ip in p for p in private) or not 16 <= address.network.prefixlen <= 30 or
            gateway not in address.network or address.ip == gateway or
            address.ip in (address.network.network_address, address.network.broadcast_address) or
            gateway in (address.network.network_address, address.network.broadcast_address)):
        fail('Give a private IPv4 /16–/30 address and a different gateway in that subnet')
    if not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}', values.get('STORAGE', '')):
        fail('Invalid container storage')
    if not re.fullmatch(r'[0-9]{1,2}', values.get('RATE_MB', '')) or not 1 <= int(values['RATE_MB']) <= 25:
        fail('Use a 1–25 MiB/s network limit')
    if values.get('PREREQUISITES') != 'true':
        fail('Confirm public DNS, port forwarding and a free static LAN address')


def budget(resources_path, meminfo_path, node):
    memory = {}
    with open(meminfo_path, encoding='utf-8') as stream:
        for line in stream:
            fields = line.split()
            if fields and fields[0] in ('MemTotal:', 'MemAvailable:'):
                memory[fields[0][:-1]] = int(fields[1]) * 1024
    total = min(memory.get('MemTotal', 0), 32 << 30)
    proposed = (512 + 4096) << 20
    with open(resources_path, encoding='utf-8') as stream:
        resources = json.load(stream)
    for row in resources:
        if row.get('node') == node and row.get('type') in ('lxc', 'qemu') and not row.get('template'):
            cap = row.get('maxmem')
            if not isinstance(cap, (int, float)) or cap <= 0 or cap > total:
                fail('Cannot verify every guest memory cap')
            proposed += int(cap)
    if proposed > total:
        fail('Guest caps plus Headscale and 4 GiB host headroom exceed host RAM; review guest caps first')
    if memory.get('MemAvailable', 0) < (512 + 4096) << 20:
        fail('Not enough available RAM to keep 4 GiB host headroom')


def network(path, cidr):
    address = ipaddress.IPv4Interface(cidr)
    with open(path, encoding='utf-8') as stream:
        interfaces = json.load(stream)
    existing = [ipaddress.IPv4Interface(f"{a['local']}/{a['prefixlen']}")
                for interface in interfaces for a in interface.get('addr_info', []) if a.get('family') == 'inet']
    if any(address.ip == a.ip for a in existing):
        fail('The address is already used by the host')
    if not any(address.network == a.network for a in existing):
        fail('The static address and prefix must match the vmbr0 subnet')


def local_storage(path):
    with open(path, encoding='utf-8') as stream:
        config = json.load(stream)
    if config.get('type') not in ('dir', 'lvm', 'lvmthin', 'zfspool', 'btrfs') or config.get('shared', 0) not in (0, False):
        fail('Controller state requires local, unshared container storage')
    if config['type'] in ('dir', 'btrfs'):
        directory = config.get('path')
        if not isinstance(directory, str) or not directory.startswith('/'):
            fail('Cannot verify local directory storage')
        print(directory)


if __name__ == '__main__':
    try:
        {'answers': answers, 'budget': budget, 'network': network, 'storage': local_storage}[sys.argv[1]](*sys.argv[2:])
    except (ValueError, KeyError, OSError, TypeError) as error:
        print(f'error: {error}', file=sys.stderr)
        sys.exit(1)
