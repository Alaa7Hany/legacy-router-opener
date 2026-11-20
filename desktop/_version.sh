#!/usr/bin/env bash
# Shared version normalization and validation for the desktop build scripts.
# Sourced by build.sh and build-macos.sh. Keep build.ps1's equivalent logic in sync.

normalize_version() {
  local raw="$1"

  # Trim surrounding whitespace (mirrors build.ps1's $Version.Trim()).
  local trimmed="${raw#"${raw%%[![:space:]]*}"}"
  trimmed="${trimmed%"${trimmed##*[![:space:]]}"}"

  # Drop an optional leading "v"/"V" so the embedded version never carries it.
  trimmed="${trimmed#v}"
  trimmed="${trimmed#V}"

  if [[ -z "$trimmed" ]]; then
    echo "Version cannot be empty." >&2
    return 1
  fi

  local core="${trimmed%%-*}"
  core="${core%%+*}"
  local -a segments=()
  IFS='.' read -r -a segments <<< "$core"
  if [[ "${#segments[@]}" -lt 1 || "${#segments[@]}" -gt 4 ]]; then
    echo "Cannot derive version segments from '$raw'." >&2
    return 1
  fi
  local segment
  for segment in "${segments[@]}"; do
    if [[ ! "$segment" =~ ^[0-9]{1,5}$ ]] || (( 10#$segment > 65535 )); then
      echo "Invalid numeric version segment '$segment' in '$raw'." >&2
      return 1
    fi
  done

  printf '%s' "$trimmed"
}
