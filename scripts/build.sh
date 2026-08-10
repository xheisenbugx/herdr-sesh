#!/bin/sh
set -eu

mkdir -p bin
exec go build -trimpath -ldflags "-s -w -X main.version=0.1.0" -o bin/herdr-sesh .

