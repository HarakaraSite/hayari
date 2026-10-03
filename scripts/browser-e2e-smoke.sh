#!/bin/sh
# browser-e2e-smoke.sh — pre-tag reference check: headless-Chromium UI smoke.
#
# This is the `browser-e2e` entry of
# .forgejo/release-profile.yml (pre_release.reference_checks). It runs in the
# documented reference environment, not in the release workflow.
#
# Requirements: Go toolchain, node, and a Playwright installation with its
# Chromium browser. The Playwright module is resolved from PLAYWRIGHT_MODULE
# (a module path such as /path/to/node_modules/playwright), falling back to
# this repository's node_modules.
set -eu

REPO_ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$REPO_ROOT"

if [ -z "${PLAYWRIGHT_MODULE:-}" ] && [ ! -d node_modules/playwright ]; then
  echo "browser-e2e: playwright not found." >&2
  echo "Set PLAYWRIGHT_MODULE to the installed playwright module path." >&2
  exit 1
fi

exec node scripts/browser-e2e-smoke.mjs
