#!/bin/zsh
set -euo pipefail

ROOT="${0:A:h:h}"
REMOTE="${1:?Usage: $0 <ssh-host> <https://cpa.example.com/desktop/v1>}"
ENDPOINT="${2:-${CPA_MENU_ENDPOINT:-}}"

if [[ -z "$ENDPOINT" ]]; then
  echo "Bridge endpoint is required" >&2
  exit 1
fi

ssh "$REMOTE" 'cat /etc/cpa-desktop-bridge.token' | swift "$ROOT/scripts/store-token.swift"
defaults write com.example.cpa-menubar bridgeEndpoint -string "$ENDPOINT"
defaults write com.example.cpa-menubar refreshMinutes -float 5
echo "Bridge connection saved to macOS Keychain"
