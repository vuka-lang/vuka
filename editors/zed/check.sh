#!/bin/sh
# Checks the Zed extension against its grammars: every query compiles, the Go
# part of the two highlights files agrees, the snippets are the VS Code
# extension's, and the grammars' sources at the revision extension.toml names
# are the working tree's. Run from the repository root after npm ci in
# grammars/tree-sitter-vuka.
set -eu

zed=editors/zed
grammars=grammars/tree-sitter-vuka
root=$PWD
ts="$root/$grammars/node_modules/.bin/tree-sitter"
err=$(mktemp)
status=0

check() { # grammar dir, language dir, sample file
	for q in "$zed/languages/$2"/*.scm; do
		if ! (cd "$grammars/$1" && "$ts" query "$root/$q" "$root/$3" >/dev/null 2>"$err"); then
			echo "$q:"
			grep -v -e 'parser directories' -e 'init-config' -e 'configuration file' -e 'language grammars' "$err"
			status=1
		fi
	done
}
check vuka vuka transpile/testdata/golden/jsx_components.vuka
check vuka_html vuka-html editors/vscode/test/fixtures-html/sales.vuka.html

go_part() { sed '/^; Markup/,$d' "$1"; }
if [ "$(go_part "$zed/languages/vuka/highlights.scm")" != "$(go_part "$zed/languages/vuka-html/highlights.scm")" ]; then
	echo "the Go part of languages/vuka-html/highlights.scm differs from languages/vuka/highlights.scm"
	status=1
fi

cmp -s editors/vscode/snippets/vuka.json "$zed/snippets/vuka.json" || { echo "snippets/vuka.json differs from the VS Code extension's"; status=1; }
cmp -s editors/vscode/snippets/vuka-html.json "$zed/snippets/vuka html.json" || { echo "snippets/vuka html.json differs from the VS Code extension's vuka-html.json"; status=1; }

for rev in $(sed -n 's/^rev = "\(.*\)"/\1/p' "$zed/extension.toml" | sort -u); do
	if ! git diff --quiet "$rev" -- "$grammars/common" "$grammars/vuka/src" "$grammars/vuka_html/src"; then
		echo "extension.toml's grammar rev $rev doesn't hold the grammars as they are: commit them and set rev to that commit"
		status=1
	fi
done
exit $status
