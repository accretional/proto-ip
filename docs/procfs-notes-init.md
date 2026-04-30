# Procfs notes

Primary /proc Files for IP Discovery

    /proc/net/fib_trie: This file contains the Forwarding Information Base (FIB) in a prefix tree format. It is the most reliable way to find all local IP addresses. Look for entries tagged as 32 host LOCAL.
        Command: awk '/32 host/ { print f } {f=$2}' /proc/net/fib_trie | grep -v 127.0.0.1.
    /proc/net/tcp: This file lists active TCP connections and listening sockets. IP addresses are represented as little-endian hexadecimal values.
        Command: cat /proc/net/tcp (The second column, local_address, contains the IP and port).
    /proc/net/if_inet6: This file specifically lists all assigned IPv6 addresses.
    /proc/net/dev: While this file lists all active network interfaces (e.g., eth0, wlan0), it does not provide their IP addresses directly.

    Primary /proc Files for IP Discovery
