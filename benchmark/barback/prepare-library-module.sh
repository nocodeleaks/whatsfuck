#!/bin/sh
# Copyright (c) 2026 Rajeh Taher
#
# Licensed under the MIT License. See LICENSE-MIT for details.

set -eu

library_dir=${1:?library directory is required}
module_path=$(sed -n 's/^module[[:space:]]\{1,\}//p' "$library_dir/go.mod" | head -n 1)

case "$module_path" in
	github.com/nocodeleaks/whatsfuck)
		go mod edit -replace=github.com/nocodeleaks/whatsfuck="$library_dir"
		;;
	go.mau.fi/whatsmeow)
		find cmd -type f -name '*.go' -exec sed -i 's#github.com/nocodeleaks/whatsfuck#go.mau.fi/whatsmeow#g' {} +
		go mod edit -dropreplace=github.com/nocodeleaks/whatsfuck
		go mod edit -droprequire=github.com/nocodeleaks/whatsfuck
		go mod edit -require=go.mau.fi/whatsmeow@v0.0.0
		go mod edit -replace=go.mau.fi/whatsmeow="$library_dir"
		;;
	*)
		echo "unsupported library module: $module_path" >&2
		exit 2
		;;
esac
