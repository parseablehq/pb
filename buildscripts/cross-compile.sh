#!/bin/bash
#
# Copyright (c) 2024 Parseable, Inc
#
# This program is free software: you can redistribute it and/or modify
# it under the terms of the GNU Affero General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.
#
# This program is distributed in the hope that it will be useful
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU Affero General Public License for more details.
#
# You should have received a copy of the GNU Affero General Public License
# along with this program.  If not, see <http://www.gnu.org/licenses/>.
#

set -e

# Enable tracing if set.
[ -n "$BASH_XTRACEFD" ] && set -x

function _init() {
    ## All binaries are static make sure to disable CGO.
    export CGO_ENABLED=0

    ## List of architectures and OS to test coss compilation.
    SUPPORTED_OSARCH="linux/amd64 linux/arm64 darwin/arm64 darwin/amd64 windows/amd64"
}

function _build() {
    local ldflags=("$@")

    # Go build to build the binary.
    export GO111MODULE=on
    export CGO_ENABLED=0
    
    for osarch in ${SUPPORTED_OSARCH}; do
        IFS=/ read -r -a arr <<<"$osarch"
        os="${arr[0]}"
        arch="${arr[1]}"
        export GOOS=$os
        export GOARCH=$arch
        printf -- "Building release binary for --> %s:%s\n" "${os}" "${arch}"
        go build -trimpath -tags kqueue --ldflags "${ldflags[@]}" -o "$(PWD)"/bin/pb_"${os}"_"${arch}"
        shasum -a 256 "$(PWD)"/bin/pb_"${os}"_"${arch}" >"$(PWD)"/bin/pb_"${os}"_"${arch}".sha256
    done
}

function main() {
    ldflags=/ read -r arr <<<"$(go run "$(PWD)"/buildscripts/gen-ldflags.go)"
    _build "${arr[@]}"
}

_init && main "$@"
