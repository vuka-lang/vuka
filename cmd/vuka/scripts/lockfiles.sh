#!/bin/sh
# Regenerates the package-lock.json vuka new writes beside each frontend
# package.json of the scaffold, with npm 11 (npm 10 can't resolve these sets
# from scratch). Run from cmd/vuka after changing a scaffold package.json:
#
#	go generate -run lockfiles .    (or: sh scripts/lockfiles.sh)
#
# TestScaffoldLockfiles fails while a lockfile and its package.json differ.
set -eu
cd "$(dirname "$0")/.."
app=scaffold/app
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

lock() { # lock <package.json source> <lockfile target>
	rm -rf "$tmp/p" && mkdir "$tmp/p"
	sed 's/{%\.Slug%}/vuka-slug/g' "$1" >"$tmp/p/package.json"
	(cd "$tmp/p" && npx --yes npm@11 install --package-lock-only --ignore-scripts --no-audit --no-fund >/dev/null)
	sed 's/vuka-slug/{%.Slug%}/g' "$tmp/p/package-lock.json" >"$2"
	echo "wrote $2"
}

lock "$app/web@islands/package.json" "$app/web@islands/package-lock.json"
for fw in vue react svelte; do
	lock "$app/frontend@$fw/package.json.tmpl" "$app/frontend@$fw/package-lock.json.tmpl"
done
