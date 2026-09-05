#!/usr/bin/env bash
# Write a GITIGNORED go.work so gopls sees every module as one build.
#
# Without it gopls guesses a build per open file and cross-module navigation is
# incomplete. It is not committed: the module reference calls committing one
# "generally inadvisable" — it overrides a contributor's own workspace and can
# make CI resolve the wrong versions. `go install pkg@version` ignores go.work
# entirely, so this can never reach a user.
set -euo pipefail
cd "$(dirname "$0")/.."

# Not mapfile: macOS ships bash 3.2, where it does not exist.
dirs=""
while IFS= read -r d; do
  dirs="$dirs $d"
done < <(find . -name go.mod -not -path './.git/*' -not -path '*/node_modules/*' -not -path './bench/*' -exec dirname {} \; | sort)

rm -f go.work go.work.sum
go work init
for d in $dirs; do
  go work use "$d"
done
echo "wrote go.work for:$dirs"
echo "(gitignored — never commit it)"
