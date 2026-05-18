#!/usr/bin/env bash

set -euo pipefail

# ─── Colors ───────────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m' # No Color

# ─── Helpers ──────────────────────────────────────────────────────────────────
error() {
	echo -e "${RED}Error: $1${NC}" >&2
	exit 1
}

info() {
	echo -e "${CYAN}$1${NC}"
}

success() {
	echo -e "${GREEN}$1${NC}"
}

warn() {
	echo -e "${YELLOW}Warning: $1${NC}" >&2
}

SU() {
	if [ "$(id -u)" -eq 0 ]; then
		"$@"
	else
		sudo "$@"
	fi
}

# Check that required tools exist
check_go() {
	if ! command -v go &>/dev/null; then
		error "Go is not installed or not in PATH. Install from https://go.dev/dl/"
	fi
}

check_git() {
	if ! command -v git &>/dev/null; then
		warn "git not found — version hash will be 'dev'"
		return 1
	fi
	if ! git rev-parse --git-dir &>/dev/null; then
		warn "Not in a git repository — version hash will be 'dev'"
		return 1
	fi
	return 0
}

# ─── Version ──────────────────────────────────────────────────────────────────
# Generate version: YYYYMMDD[SUFFIX][-HASH]
# Suffixes: HF (hotfix), CVE (security), DEV (beta), DEP (dependency update)
# If not a release build, appends the short commit hash (or "dev" if unavailable).
gen_version() {
	local suffix="${1:-DEV}"
	local date
	date=$(date +%Y%m%d)
	local hash="dev"
	if check_git 2>/dev/null; then
		hash=$(git rev-parse --short=7 HEAD 2>/dev/null || echo "dev")
	fi
	echo "${date}${suffix}-${hash}"
}

# ─── Build ────────────────────────────────────────────────────────────────────
build() {
	local suffix="${1:-DEV}"
	local version
	version=$(gen_version "$suffix")
	local ldflags="-s -w -X 'asika/common/version.Version=${version}' -X 'asika/common/version.Enabled=true'"

	check_go

	info "Building with version: ${BOLD}${version}${NC} (stripped)"

	local start=$SECONDS
	env go build -ldflags="${ldflags}" -o asikad ./cmd/asikad/main.go
	info "  asikad ✓ ($(( SECONDS - start ))s)"

	start=$SECONDS
	env go build -ldflags="${ldflags}" -o asika ./cmd/asika/main.go
	info "  asika  ✓ ($(( SECONDS - start ))s)"

	strip asika asikad 2>/dev/null || warn "strip not available — binaries not stripped"

	success "Build complete: asika, asikad"
}

# Build without stripping (debug symbols preserved)
build_debug() {
	local version
	version=$(gen_version "DEV")
	local ldflags="-X 'asika/common/version.Version=${version}' -X 'asika/common/version.Enabled=true'"

	check_go

	info "Building ${BOLD}debug${NC} binaries with version: ${version}"

	local start=$SECONDS
	env go build -ldflags="${ldflags}" -o asikad ./cmd/asikad/main.go
	info "  asikad ✓ ($(( SECONDS - start ))s)"

	start=$SECONDS
	env go build -ldflags="${ldflags}" -o asika ./cmd/asika/main.go
	info "  asika  ✓ ($(( SECONDS - start ))s)"

	success "Debug build complete: asika, asikad (symbols preserved)"
}

# ─── Dependencies ─────────────────────────────────────────────────────────────
dep() {
	check_go
	info "Downloading dependencies..."
	go mod download
	go mod tidy
	success "Dependencies ready"
}

# ─── Lint ─────────────────────────────────────────────────────────────────────
lint() {
	check_go
	info "Running go fmt..."
	go fmt ./...
	success "Formatting OK"

	info "Running go vet..."
	go vet ./...
	success "Vet OK"
}

# ─── Test ─────────────────────────────────────────────────────────────────────
run_tests() {
	check_go
	local test_flags=("$@")
	info "Running tests..."
	if go test "${test_flags[@]}" ./...; then
		success "All tests passed"
	else
		error "Tests failed"
	fi
}

# ─── Clean ────────────────────────────────────────────────────────────────────
clean() {
	info "Cleaning build artifacts..."
	rm -rf asika* asikad*
	success "Clean complete"
}

distclean() {
	clean
	info "Cleaning Go build cache..."
	rm -rf ~/.cache/go-build
	SU rm -rf ~/go
	success "Distclean complete"
}

# ─── Serve ────────────────────────────────────────────────────────────────────
serve() {
	if [ ! -f ./asikad ]; then
		error "asikad binary not found. Run 'bash build.sh build' first."
	fi
	SU nohup ./asikad > asikad.log 2>&1 &
	success "Daemon started (asikad)"
}

stop() {
	SU killall asikad 2>/dev/null || warn "asikad was not running"
	success "Daemon stopped"
}

# ─── Version Info ─────────────────────────────────────────────────────────────
show_version() {
	check_go
	info "Go version:     $(go version)"
	info "Project module: $(head -1 go.mod | awk '{print $2}')"
	info "Module requires: $(grep '^go ' go.mod | awk '{print $2}')"

	if check_git 2>/dev/null; then
		info "Git branch:     $(git rev-parse --abbrev-ref HEAD 2>/dev/null)"
		info "Git commit:     $(git rev-parse --short=7 HEAD 2>/dev/null)"
		info "Build version:  $(gen_version)"
	fi

	if [ -f ./asika ]; then
		info "CLI binary:     $(file ./asika | sed 's/.*: //')"
	fi
	if [ -f ./asikad ]; then
		info "Daemon binary:  $(file ./asikad | sed 's/.*: //')"
	fi
}

# ─── Cross Compile ────────────────────────────────────────────────────────────
cross_build() {
	check_go

	local platforms=(
		"linux:amd64"
		"linux:arm64"
		"darwin:amd64"
		"darwin:arm64"
		"windows:amd64"
	)

	local version
	version=$(gen_version "REL")
	local ldflags="-s -w -X 'asika/common/version.Version=${version}' -X 'asika/common/version.Enabled=true'"

	local outdir="dist"
	mkdir -p "$outdir"

	info "Cross-compiling for ${#platforms[@]} platforms in parallel (version: ${version})"

	local tmpdir
	tmpdir=$(mktemp -d)
	local pids=()

	for platform in "${platforms[@]}"; do
		local GOOS="${platform%%:*}"
		local GOARCH="${platform##*:}"
		local suffix="${GOOS}-${GOARCH}"
		local ext=""
		[ "$GOOS" = "windows" ] && ext=".exe"

		(
			local start=$SECONDS
			local logfile="${tmpdir}/${suffix}.log"
			local fail=0

			if CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
				go build -ldflags="${ldflags}" -o "${outdir}/asikad-${suffix}${ext}" ./cmd/asikad/main.go 2>"$logfile"; then
				echo "  asikad-${suffix} ✓ ($(( SECONDS - start ))s)" >> "$logfile"
			else
				echo "  asikad-${suffix} ✗" >> "$logfile"
				fail=1
			fi

			start=$SECONDS
			if CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
				go build -ldflags="${ldflags}" -o "${outdir}/asika-${suffix}${ext}" ./cmd/asika/main.go 2>>"$logfile"; then
				echo "  asika-${suffix}  ✓ ($(( SECONDS - start ))s)" >> "$logfile"
			else
				echo "  asika-${suffix}  ✗" >> "$logfile"
				fail=1
			fi

			exit "$fail"
		) &
		pids+=($!)
	done

	local failed=0
	for i in "${!pids[@]}"; do
		local platform="${platforms[$i]}"
		local suffix="${platform%%:*}-${platform##*:}"
		if wait "${pids[$i]}"; then
			cat "${tmpdir}/${suffix}.log"
		else
			cat "${tmpdir}/${suffix}.log"
			(( failed++ ))
		fi
	done

	rm -rf "$tmpdir"

	if [ "$failed" -gt 0 ]; then
		error "Cross-compile completed with ${failed} failure(s). See ${outdir}/"
	fi

	success "Cross-compile complete. Binaries in ${outdir}/"
}

# ─── Help ─────────────────────────────────────────────────────────────────────
usage() {
	cat <<EOF
${BOLD}asika build script${NC}

${BOLD}USAGE${NC}
    bash build.sh [command] [options]

${BOLD}COMMANDS${NC}
    ${CYAN}build${NC}              Build stripped binaries (default)
    ${CYAN}build-debug${NC}        Build with debug symbols preserved
    ${CYAN}dep${NC}                Download and tidy dependencies
    ${CYAN}lint${NC}               Run go fmt and go vet
    ${CYAN}test${NC}               Run all tests (with optional flags)
    ${CYAN}clean${NC}              Remove build artifacts (asika, asikad)
    ${CYAN}distclean${NC}          Deep clean (includes Go module cache)
    ${CYAN}cross${NC}              Cross-compile for all platforms → dist/
    ${CYAN}serve${NC}              Start asikad daemon in background
    ${CYAN}stop${NC}               Stop asikad daemon
    ${CYAN}version${NC}            Show toolchain and version info

${BOLD}EXAMPLES${NC}
    bash build.sh                        # Build stripped binaries
    bash build.sh build                  # Same as above
    bash build.sh build-debug             # Debug build (no strip)
    bash build.sh test -race             # Run tests with race detector
    bash build.sh test -short            # Run tests, skip long ones
    bash build.sh test -run TestFoo      # Run specific test
    bash build.sh cross                  # Cross-compile all platforms

${BOLD}VERSION SUFFIXES${NC}
    The build command accepts an optional suffix via environment:
    SUFFIX=HF bash build.sh build         # Hotfix build
    SUFFIX=CVE bash build.sh build        # Security fix build
    SUFFIX=DEV bash build.sh build        # Development build (default)
    SUFFIX=DEP bash build.sh build        # Dependency update build

EOF
}

# ─── Main ─────────────────────────────────────────────────────────────────────
cmd="${1:-build}"
shift || true

case "$cmd" in
	build)
		build "${SUFFIX:-DEV}"
		;;
	build-debug|debug)
		build_debug
		;;
	dep)
		dep
		;;
	lint)
		lint
		;;
	test)
		lint && run_tests "$@"
		;;
	clean)
		clean
		;;
	distclean)
		distclean
		;;
	cross)
		cross_build
		;;
	serve)
		serve
		;;
	stop)
		stop
		;;
	version|--version|-v)
		show_version
		;;
	help|--help|-h)
		usage
		;;
	*)
		error "Unknown command: ${cmd}\nRun 'bash build.sh help' for usage."
		;;
esac
