#!/usr/bin/env bash
# Offline end-to-end examples, also run by CI. All outputs stay in a temp dir.
set -euo pipefail
m5_dir=$(mktemp -d)
trap 'rm -rf "$m5_dir"' EXIT
go build -o "$m5_dir/rexc" ./cmd/rexc
go build -o "$m5_dir/rexd" ./cmd/rexd
"$m5_dir/rexc" -rules examples/m5/rules.json -output "$m5_dir/rules.bytecode"
test -s "$m5_dir/rules.bytecode.manifest.json"
"$m5_dir/rexc" explain -artifact "$m5_dir/rules.bytecode" > "$m5_dir/explain.json"
"$m5_dir/rexc" bundle -rules examples/m5/rules.json -scenario examples/m5/scenario.json > "$m5_dir/bundle.json"
"$m5_dir/rexc" simulate -bundle "$m5_dir/bundle.json" > "$m5_dir/first.json"
"$m5_dir/rexc" simulate -bundle "$m5_dir/bundle.json" > "$m5_dir/second.json"
cmp "$m5_dir/first.json" "$m5_dir/second.json"
# No Redis process/configuration is involved, even with deliberately invalid settings.
REDIS_ADDRESS=invalid:0 "$m5_dir/rexd" --dry-run --bundle "$m5_dir/bundle.json" > "$m5_dir/dry-run.json"
cmp "$m5_dir/first.json" "$m5_dir/dry-run.json"
"$m5_dir/rexc" test -rules examples/m5/rules.json -scenario examples/m5/suite.json > "$m5_dir/tests.json"
"$m5_dir/rexc" lint -rules examples/m5/rules.json > "$m5_dir/lint.json"
"$m5_dir/rexc" compare -rules examples/m5/rules.json -bundle "$m5_dir/bundle.json" > "$m5_dir/same.json"
comparison_status=0
"$m5_dir/rexc" compare -rules examples/m5/candidate.json -bundle "$m5_dir/bundle.json" > "$m5_dir/changed.json" || comparison_status=$?
test "$comparison_status" -eq 2
printf 'M5 offline CLI examples passed\n'
