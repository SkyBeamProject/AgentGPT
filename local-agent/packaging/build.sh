#!/usr/bin/env bash
# Build the Linux binary, the Windows .exe, the NSIS setup .exe, and the MSI.
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$PWD"
REPO="$(cd "$ROOT/.." && pwd)"
VERSION="$(tr -d '[:space:]' < packaging/VERSION)"
DIST="$ROOT/dist"
STAGE="$DIST/stage"
rm -rf "$DIST"
mkdir -p "$STAGE"

python3 packaging/make_assets.py

IFS='.' read -r MAJOR MINOR PATCH <<<"${VERSION}"
PATCH="${PATCH%%[^0-9]*}"
cat > packaging/windows/beam-agent.rc <<EOF
1 ICON "beam-agent.ico"

1 VERSIONINFO
FILEVERSION ${MAJOR},${MINOR},${PATCH},0
PRODUCTVERSION ${MAJOR},${MINOR},${PATCH},0
FILEFLAGSMASK 0x3fL
FILEFLAGS 0
FILEOS 0x40004L
FILETYPE 0x1L
BEGIN
  BLOCK "StringFileInfo"
  BEGIN
    BLOCK "040904B0"
    BEGIN
      VALUE "CompanyName", "SkyBeam Project"
      VALUE "FileDescription", "Beam Agent"
      VALUE "FileVersion", "${VERSION}"
      VALUE "InternalName", "beam-agent"
      VALUE "LegalCopyright", "GPL-3.0-or-later"
      VALUE "OriginalFilename", "beam-agent.exe"
      VALUE "ProductName", "Beam Agent"
      VALUE "ProductVersion", "${VERSION}"
    END
  END
  BLOCK "VarFileInfo"
  BEGIN
    VALUE "Translation", 0x409, 1200
  END
END
EOF

(
  cd packaging/windows
  x86_64-w64-mingw32-windres -O coff -i beam-agent.rc -o "$ROOT/cmd/beam-agent/rsrc_windows_amd64.syso"
)

export CGO_ENABLED=0
go test ./...
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o "$DIST/beam-agent-linux-amd64" ./cmd/beam-agent
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o "$DIST/beam-agent-windows-amd64.exe" ./cmd/beam-agent
rm -f "$ROOT/cmd/beam-agent/rsrc_windows_amd64.syso"

SMOKE="$(mktemp -d)"
HOME="$SMOKE" XDG_CONFIG_HOME="$SMOKE/cfg" \
  "$DIST/beam-agent-linux-amd64" run --goal "Leave a note" --workspace "$SMOKE/ws" --mock >/tmp/beam-agent-smoke.txt
grep -q "Practice run finished" /tmp/beam-agent-smoke.txt
test -f "$SMOKE/ws/beam-practice-note.txt"
"$DIST/beam-agent-linux-amd64" version | grep -q "$VERSION"

cp "$DIST/beam-agent-windows-amd64.exe" "$STAGE/beam-agent.exe"
cp "$REPO/LICENSE" "$STAGE/LICENSE.txt"
cat > "$STAGE/SOURCE.txt" <<EOF
Beam Agent source code is the local-agent directory of
https://github.com/SkyBeamProject/AgentGPT
and is licensed under the GNU General Public License version 3.
EOF
cp packaging/windows/beam-agent.nsi packaging/windows/beam-agent.wxs packaging/windows/beam-agent.ico "$STAGE/"

(
  cd "$STAGE"
  makensis -DVERSION="$VERSION" beam-agent.nsi
  wixl --arch x64 -D ProductVersion="${VERSION}.0" -o BeamAgent.msi beam-agent.wxs
)

mv "$STAGE/BeamAgentSetup.exe" "$STAGE/BeamAgent.msi" "$DIST/"
rm -rf "$STAGE"
(
  cd "$DIST"
  sha256sum beam-agent-linux-amd64 beam-agent-windows-amd64.exe BeamAgentSetup.exe BeamAgent.msi > SHA256SUMS
)
echo "Built installers in $DIST"
