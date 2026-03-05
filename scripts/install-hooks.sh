#!/bin/sh
# Installs project git hooks into .git/hooks/.
# Run once after cloning: sh scripts/install-hooks.sh

REPO_ROOT=$(git rev-parse --show-toplevel)
HOOKS_SRC="$REPO_ROOT/scripts/hooks"
HOOKS_DST="$REPO_ROOT/.git/hooks"

for hook in "$HOOKS_SRC"/*; do
  name=$(basename "$hook")
  target="$HOOKS_DST/$name"
  ln -sf "$hook" "$target"
  chmod +x "$hook"
  echo "Installed: .git/hooks/$name -> $hook"
done

echo "Done. Git hooks are active."
