#!/usr/bin/env bash
# Exercises `make lint-md` against a throwaway repo.
set -euo pipefail
mk="$(cd "$(dirname "$0")/.." && pwd)/Makefile"
dir=$(mktemp -d "${TMPDIR:-/tmp}/lint-md.XXXXXX")
trap 'rm -rf "$dir"' EXIT
cd "$dir"
git init -q
git -c user.name=t -c user.email=t@t commit -q --allow-empty -m init

rep() { perl -CSDA -e 'print $ARGV[0] x $ARGV[1]' "$1" "$2"; }
fail=0
check() { # name, want (pass|fail), file content
  git rm -rqf --cached . >/dev/null 2>&1 || true
  printf '%s\n' "$3" >t.md
  git add t.md
  if out=$(make -s -f "$mk" lint-md 2>&1); then got=pass; else got=fail; fi
  if [ "$got" != "$2" ]; then echo "FAIL $1: want $2, got $got: $out"; fail=1; fi
}

check "72 ascii" pass "$(rep a 72)"
check "73 ascii" fail "$(rep a 73)"
check "72 non-ascii" pass "$(rep ö 72)"
check "73 non-ascii" fail "$(rep ö 73)"
check "72 ascii crlf" pass "$(rep a 72)"$'\r'
check "leading plus" fail "++ $(rep a 71)"

git rm -rqf --cached . >/dev/null
make -s -f "$mk" lint-md || { echo "FAIL empty index"; fail=1; }

printf '%s\n' "$(rep a 80)" >old.md
git add old.md
git -c user.name=t -c user.email=t@t commit -q -m old
printf 'short\n' >>old.md
git add old.md
make -s -f "$mk" lint-md || { echo "FAIL untouched long line"; fail=1; }

out=$(printf '%s\n' "$(rep b 73)" >>old.md; git add old.md; make -s -f "$mk" lint-md 2>&1) && fail=1
case $out in *old.md*bbb*) ;; *) echo "FAIL output lacks file/line: $out"; fail=1 ;; esac

rm -rf .git
make -s -f "$mk" lint-md >/dev/null 2>&1 && { echo "FAIL passes outside a repo"; fail=1; }

exit $fail
