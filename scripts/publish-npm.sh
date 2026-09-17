#!/usr/bin/env bash
#
# Publish the CLI to npm, from the binaries GoReleaser just built.
#
# Six packages go out. Five hold one binary each and are filtered by npm's
# os/cpu fields, so a Mac never downloads the Windows build. The sixth is the
# launcher a developer actually installs; it depends on the other five as
# optional dependencies and runs whichever one npm kept.
#
# Run from the repo root, after `goreleaser release`, with dist/ still in place:
#
#   scripts/publish-npm.sh 0.2.3          # publish as `latest`
#   scripts/publish-npm.sh 0.2.3 --next   # publish as `next`, leave `latest`
#
# `latest` by default, because Homebrew and Scoop are updated the moment a
# release is cut and npm should not be the one channel that needs somebody to
# remember a second command. That is how it ended up three releases behind.
#
# Use --next when the risk is in this script rather than in the binary: a
# change to how the packages are built, or a first run against the registry.

set -euo pipefail

VERSION="${1:-}"
MODE="${2:-}"

if [[ -z "$VERSION" ]]; then
  echo "usage: $0 <version> [--next]" >&2
  exit 2
fi

if [[ -n "$MODE" && "$MODE" != "--next" ]]; then
  echo "error: unknown option '$MODE' (only --next)" >&2
  exit 2
fi

# GoReleaser hands the version through without the v; a tag pasted by hand
# usually has one. Strip it rather than publishing 'vx.y.z' to npm, which is
# not a valid version and fails late with a confusing message.
VERSION="${VERSION#v}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="$REPO_ROOT/dist"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

if [[ ! -d "$DIST" ]]; then
  echo "error: no dist/ — run goreleaser first." >&2
  exit 1
fi

# npm's platform names are not Go's. Each row is:
#   <npm suffix> <goreleaser os> <goreleaser arch> <npm os> <npm cpu>
PLATFORMS=(
  "darwin-arm64 darwin  arm64 darwin arm64"
  "darwin-x64   darwin  amd64 darwin x64"
  "linux-arm64  linux   arm64 linux  arm64"
  "linux-x64    linux   amd64 linux  x64"
  "win32-x64    windows amd64 win32  x64"
)

publish() {
  local dir="$1"
  # --access public because the scope is private by default, and a first
  # publish without it fails. Harmless on later ones.
  local args=(publish --access public)
  if [[ "$MODE" == "--next" ]]; then
    args+=(--tag next)
  else
    args+=(--tag latest)
  fi
  ( cd "$dir" && npm "${args[@]}" )
}

echo "Building npm packages for $VERSION"

for row in "${PLATFORMS[@]}"; do
  read -r suffix goos goarch npmos npmcpu <<<"$row"

  binary="bachs"
  archive="$DIST/bachs_${VERSION}_${goos}_${goarch}.tar.gz"
  if [[ "$goos" == "windows" ]]; then
    binary="bachs.exe"
    archive="$DIST/bachs_${VERSION}_${goos}_${goarch}.zip"
  fi

  if [[ ! -f "$archive" ]]; then
    echo "error: missing $archive" >&2
    echo "The version must match the one goreleaser built." >&2
    exit 1
  fi

  pkg="$WORK/cli-$suffix"
  mkdir -p "$pkg/bin"

  if [[ "$goos" == "windows" ]]; then
    unzip -q -j "$archive" "$binary" -d "$pkg/bin"
  else
    tar -xzf "$archive" -C "$pkg/bin" "$binary"
  fi
  chmod +x "$pkg/bin/$binary"

  cat > "$pkg/package.json" <<JSON
{
  "name": "@bachs/cli-$suffix",
  "version": "$VERSION",
  "description": "Bachs CLI binary for $npmos $npmcpu. Installed automatically by @bachs/cli.",
  "license": "MIT",
  "os": ["$npmos"],
  "cpu": ["$npmcpu"],
  "files": ["bin/$binary"],
  "repository": { "type": "git", "url": "git+https://github.com/bachsdev/bachs-cli.git" }
}
JSON

  publish "$pkg"
  echo "  published @bachs/cli-$suffix@$VERSION"
done

# The launcher is the repo's npm/package.json with the versions stamped in.
# Generated rather than edited, because the version appears six times in it and
# hand-editing is what left npm three releases behind in the first place.
launcher="$WORK/cli"
mkdir -p "$launcher/bin"
cp "$REPO_ROOT/npm/bin/bachs.js" "$launcher/bin/bachs.js"

VERSION="$VERSION" python3 - "$REPO_ROOT/npm/package.json" "$launcher/package.json" <<'PY'
import json, os, sys

source, target = sys.argv[1], sys.argv[2]
version = os.environ["VERSION"]

with open(source) as handle:
    package = json.load(handle)

package["version"] = version
package["optionalDependencies"] = {
    name: version for name in package.get("optionalDependencies", {})
}

if not package["optionalDependencies"]:
    sys.exit("error: npm/package.json lists no optionalDependencies")

with open(target, "w") as handle:
    json.dump(package, handle, indent=2, ensure_ascii=False)
    handle.write("\n")
PY

publish "$launcher"
echo "  published @bachs/cli@$VERSION"

echo
if [[ "$MODE" == "--next" ]]; then
  cat <<NEXT
Done, under the \`next\` tag. \`latest\` has not moved, so a plain
\`npm install\` still gets the old version.

Check it, then promote it:

  npm install @bachs/cli@next
  ./node_modules/.bin/bachs --version

  npm dist-tag add @bachs/cli@$VERSION latest
NEXT
else
  echo "Done. @bachs/cli@$VERSION is now what a plain npm install gets."
fi
