#!/bin/bash
# fix_imports.sh - Fix import paths after merging from upstream
# Replaces upstream import paths with your fork's import path

set -e

UPSTREAM_PATH="github.com/Dicklesworthstone/ntm"
FORK_PATH="github.com/shahbajlive/ntm"

echo "Fixing import paths from $UPSTREAM_PATH to $FORK_PATH..."

# Find all .go files except vendor and .git directories
find . -name "*.go" -type f ! -path "./vendor/*" ! -path "./.git/*" | while read -r file; do
  # Use sed to replace import paths
  # On macOS, sed -i requires an extension, so we use a temp file approach
  if [[ "$OSTYPE" == "darwin"* ]]; then
    sed -i '' "s|${UPSTREAM_PATH}|${FORK_PATH}|g" "$file"
  else
    sed -i "s|${UPSTREAM_PATH}|${FORK_PATH}|g" "$file"
  fi
done

echo "Import paths fixed. Running go mod tidy..."
go mod tidy

echo "Done!"
