#!/usr/bin/env bash
# Enforces the Go coverage gates from AGENTS.md section 5.4 on a cover profile.
# Generated files, cmd/ (wiring), internal/testutil and migrations are excluded.
# shortcut: internal/llm is gated at 90% as a whole, not only its context builder and validation; split once the package grows.
set -euo pipefail

profile=${1:-coverage.out}
module=$(go list -m)
generated=$(grep -rlE --include='*.go' --exclude-dir=node_modules '^// Code generated .* DO NOT EDIT\.$' . | sed "s|^\./|$module/|" || true)

awk -v module="$module" -v generated="$generated" '
BEGIN {
  n = split(generated, files, "\n")
  for (i = 1; i <= n; i++) skip[files[i]] = 1
}
FNR == 1 { next } # "mode:" line
{
  split($1, loc, ":")
  file = loc[1]
  if (file in skip) next
  pkg = file
  sub(/\/[^\/]*$/, "", pkg)
  rel = substr(pkg, length(module) + 2)
  if (rel ~ /^cmd\// || rel == "internal/testutil" || rel == "migrations") next
  # A block can appear once per test binary; count it once, covered if any run hit it.
  if (!($1 in seen)) { seen[$1] = 1; total[rel] += $2 }
  if ($3 > 0 && !($1 in hit)) { hit[$1] = 1; covered[rel] += $2 }
}
END {
  status = 0
  for (rel in total) {
    if (total[rel] == 0) continue
    gate = (rel ~ /^internal\/(analytics|importer|llm)(\/|$)/) ? 90 : 80
    pct = 100 * covered[rel] / total[rel]
    printf "%-4s %5.1f%% (gate %d%%) %s\n", (pct < gate ? "FAIL" : "ok"), pct, gate, rel
    if (pct < gate) status = 1
  }
  exit status
}' "$profile" | sort -k4
