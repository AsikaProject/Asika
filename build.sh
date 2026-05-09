#!/usr/bin/env bash

set -eu

error() {
	echo "Error: $1" >&2
	exit 1
}

SU() {
	if [ "$(id -u)" -eq 0 ]; then
		"$@"
	else
		sudo "$@"
	fi
}

# Generate version: YYYYMMDD[SUFFIX][-HASH]
# Suffixes: HF (hotfix), CVE (security), DEV (beta), DEP (dependency update)
# If not a release build, appends the short commit hash (or "dev" if unavailable).
gen_version() {
	local suffix="${1:-DEV}"
	local date
	date=$(date +%Y%m%d)
	local hash
	hash=$(git rev-parse --short=7 HEAD 2>/dev/null || echo "dev")
	echo "${date}${suffix}-${hash}"
}

# Build with strip (default)
build() {
	local version
	version=$(gen_version)
	local ldflags="-s -w -X 'asika/common/version.Version=${version}'"
	echo "Building with version: ${version} (stripped)"
	env go build -ldflags="${ldflags}" -o asikad ./cmd/asikad/main.go
	env go build -ldflags="${ldflags}" -o asika ./cmd/asika/main.go
	strip asika
	strip asikad
}

# Download dependencies
dep() {
	echo "Downloading dependencies..."
	go mod download
}

clean() {
	rm -rf asika* asikad*
}

distclean() {
	clean
	rm -rf ~/.cache/go-build
	SU rm -rf ~/go
}

serve() {
	SU nohup ./asikad > asikad.log 2>&1 & echo ok
}

stop() {
	SU killall asikad
}

lint () {
	go fmt
	go vet ./...
}

test () {
	go test ./...
}

# Parse command line arguments
case "${1:-build}" in
	build)
		build
		;;
	dep)
		dep
		;;
	clean)
		clean
		;;
	distclean)
		distclean
		;;
	serve)
		serve
		;;
	stop)
		stop
		;;
	test)
		lint && test
		;;
	*)
		error "Unknown command: $1 (use: build, dep, clean, distclean)"
		;;
esac
