#!/usr/bin/env bash
set -euo pipefail

DOCS_DIR="${1:-docs}"
SITE_DIR="${2:-site/content/docs}"

declare -A TITLE
TITLE[design.md]="Design and Architecture"
TITLE[configuration.md]="Configuration Reference"
TITLE[deployment.md]="Deployment"
TITLE[inspectors.md]="Inspectors"
TITLE[operations.md]="Operations"
TITLE[client-config.md]="Client Configuration"

declare -A WEIGHT
WEIGHT[design.md]=1
WEIGHT[configuration.md]=3
WEIGHT[deployment.md]=2
WEIGHT[inspectors.md]=4
WEIGHT[operations.md]=5
WEIGHT[client-config.md]=6

declare -A ADR_TITLE
ADR_TITLE[001-why-go.md]="ADR 1: Why Go"
ADR_TITLE[002-why-proxy.md]="ADR 2: Why a proxy, not a library"
ADR_TITLE[003-why-three-formats.md]="ADR 3: Why three API formats"
ADR_TITLE[004-why-fail-open.md]="ADR 4: Why fail-open on inspector errors"
ADR_TITLE[005-why-user-tokens.md]="ADR 5: Why user tokens pass through"
ADR_TITLE[006-why-scratch.md]="ADR 6: Why scratch Docker runtime"
ADR_TITLE[007-mode-naming.md]="ADR 007: Response mode naming"
ADR_TITLE[008-eicar-test-strings.md]="ADR 008: EICAR-style test strings"

mkdir -p "$SITE_DIR/adr"

for f in "$DOCS_DIR"/*.md; do
  base=$(basename "$f")
  title="${TITLE[$base]:-}"
  weight="${WEIGHT[$base]:-}"
  if [ -z "$title" ]; then
    continue
  fi
  {
    echo '+++'
    echo "title = \"$title\""
    echo "sort_by = \"weight\""
    echo "weight = $weight"
    echo '+++'
    echo ""
    sed '1{/^# /d}' "$f"
  } > "$SITE_DIR/$base"
done

for f in "$DOCS_DIR/adr"/*.md; do
  base=$(basename "$f")
  title="${ADR_TITLE[$base]:-}"
  weight="${base%%-*}"
  weight=$((10#$weight))
  if [ -z "$title" ]; then
    continue
  fi
  {
    echo '+++'
    echo "title = \"$title\""
    echo "weight = $weight"
    echo '+++'
    echo ""
    sed '1{/^# /d}' "$f"
  } > "$SITE_DIR/adr/$base"
done

echo "Synced docs to $SITE_DIR"
