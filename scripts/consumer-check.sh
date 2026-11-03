#!/bin/sh
set -eu

repo_root=$(CDPATH= cd "$(dirname "$0")/.." && pwd)
expected_module_path=github.com/hffix-go/hffix-go
module_path=$(cd "$repo_root" && GOWORK=off go list -m -f '{{.Path}}')

if [ "$module_path" != "$expected_module_path" ]; then
	printf 'consumer check: expected module path %s, got %s\n' \
		"$expected_module_path" "$module_path" >&2
	exit 1
fi

tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/hffix-consumer-check.XXXXXX")
trap 'rm -rf "$tmpdir"' EXIT HUP INT TERM

(
	cd "$tmpdir"
	go mod init example.com/hffix-consumer-check >/dev/null
	go mod edit -require="$expected_module_path@v0.0.0"
	go mod edit -replace="$expected_module_path=$repo_root"
	cp "$repo_root/examples/quickstart/main.go" main.go
	go mod tidy
	GOWORK=off GOFLAGS=-mod=mod go run .
)

printf 'consumer check passed\n'
