#!/usr/bin/env bash
# Documentation site helper. Every command regenerates the site content from
# docs/ first (go run ./cmd/gen-site), so what you see always matches docs/.
#
#   site.sh gen       regenerate website/src/content and the README install region
#   site.sh dev       live-reloading dev server at http://localhost:4321/gskill/
#   site.sh build     production build into website/dist (validates links)
#   site.sh preview   build, then serve website/dist at http://localhost:4321/gskill/
#   site.sh check     what the CI site job runs: drift check, build, landing example
#
# Needs Go (see go.mod) and Node.js 24. Docs: docs/contributing/website.md.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

cd "$repo_root"
web="${repo_root}/website"

need_node_modules() {
	have npm || die "missing npm: install Node.js 24"
	if [ ! -d "${web}/node_modules" ]; then
		section "site: npm ci"
		npm ci --prefix "$web"
	fi
}

gen() {
	section "site: generate content from docs/"
	go run ./cmd/gen-site >/dev/null
}

case "${1:-}" in
gen)
	gen
	;;
dev)
	gen
	need_node_modules
	npm run dev --prefix "$web"
	;;
build)
	gen
	need_node_modules
	section "site: build"
	npm run build --prefix "$web"
	;;
preview)
	gen
	need_node_modules
	section "site: build"
	npm run build --prefix "$web"
	npm run preview --prefix "$web"
	;;
check)
	section "site: README install region drift"
	go run ./cmd/gen-site -check
	gen
	need_node_modules
	section "site: build"
	npm run build --prefix "$web"
	"${repo_root}/scripts/check-landing-example.sh" --local
	;;
*)
	die "usage: $(basename "$0") gen | dev | build | preview | check"
	;;
esac
