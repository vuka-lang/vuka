#!/bin/sh
# Builds the playground's compiler into public/playground: vuka.wasm, the
# wasm_exec.js of the same Go, and types.bin, the export data of std, ui, web
# and orm at the versions testdata/gomode pins. One Go makes all three: the
# one gomode's go.mod selects.
set -eu
cd "$(dirname "$0")/.."
GOTOOLCHAIN=$(cd testdata/gomode && go env GOVERSION)
export GOTOOLCHAIN
out=public/playground
mkdir -p "$out"
(cd .. && go run ./cmd/vuka-wasm -o "docs/$out/types.bin")
(cd .. && GOOS=js GOARCH=wasm go build -trimpath -ldflags=-s -o "docs/$out/vuka.wasm" ./cmd/vuka-wasm)
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" "$out/wasm_exec.js"
ls -l "$out/vuka.wasm" "$out/types.bin"
