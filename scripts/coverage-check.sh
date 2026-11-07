#!/bin/sh
# Enforces per-package statement-coverage floors so the suite acts as a
# ratchet: refactors must not silently drop covered behavior. Bump a floor
# when new tests raise a package's coverage.
set -eu

MIN_P2P="${MIN_P2P:-75}"
MIN_PROTOCOLS="${MIN_PROTOCOLS:-70}"
MIN_DOMAIN="${MIN_DOMAIN:-40}"
MIN_CONFIG="${MIN_CONFIG:-80}"
MIN_IPC_CLIENT="${MIN_IPC_CLIENT:-13}"
MIN_IPC_SERVER="${MIN_IPC_SERVER:-60}"

fail=0
check() {
	pkg="$1"
	min="$2"
	line=$(go test -cover -count=1 "$pkg" 2>&1 | tail -1)
	case "$line" in
		*"no test files"*)
			echo "SKIP $pkg (no test files)"
			return 0
			;;
	esac
	cov=$(printf '%s' "$line" | sed -n 's/.*coverage: \([0-9.]*\)%.*/\1/p')
	if [ -z "$cov" ]; then
		echo "FAIL $pkg: could not parse coverage from: $line"
		fail=1
		return 0
	fi
	if awk "BEGIN{exit !( $cov < $min )}"; then
		echo "FAIL $pkg: coverage ${cov}% below floor ${min}%"
		fail=1
	else
		echo "ok   $pkg ${cov}% (>= ${min}%)"
	fi
}

check ./internal/infra/p2p/ "$MIN_P2P"
check ./internal/infra/p2p/protocols/ "$MIN_PROTOCOLS"
check ./internal/domain/ "$MIN_DOMAIN"
check ./internal/config/ "$MIN_CONFIG"
check ./internal/infra/ipc/client/ "$MIN_IPC_CLIENT"
check ./internal/infra/ipc/server/ "$MIN_IPC_SERVER"

exit "$fail"
