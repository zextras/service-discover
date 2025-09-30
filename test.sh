#!/bin/bash

dirs=(
    'pkg/encrypter'
    'pkg/exec'
    'pkg/formatter'
    'pkg/parser'
    'pkg/carbonio'
    'pkg/command'
    'cmd/agent'
    'cmd/server'
)
for i in ${dirs[@]}; do
    (
        echo "Entering directory $i"
        cd "$i" || true
        go run gotest.tools/gotestsum@latest --format testname --junitfile tests.xml
    )
done