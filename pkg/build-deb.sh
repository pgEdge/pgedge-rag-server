#!/usr/bin/env bash
set -euo pipefail

# Environment variables
BUILD_DIR="/tmp/pg_deb_build"

CWD="$(pwd)"

export DEBIAN_FRONTEND=noninteractive
ARCH=$(uname -m)
if [ "$ARCH" = "aarch64" ]; then
  ARCH="arm64"
fi

# Release assets are named with the full tag version (e.g. 1.0.0-beta3).
# RAG_SERVER_VERSION may carry a '~beta…' suffix for DEB ordering, so it must
# NOT be used to build the download URL — use the tag instead.
TAG_VERSION="${RAG_SERVER_BRANCH#v}"
ARTIFACT_DIR="${ARTIFACT_DIR:-${CWD}/release-artifacts}"
SRC_DIR="${BUILD_DIR}/pgedge-rag-server_${TAG_VERSION}"
RELEASE_URL="https://github.com/pgEdge/pgedge-rag-server/releases/download/${RAG_SERVER_BRANCH}"
RAW_URL="https://raw.githubusercontent.com/pgEdge/pgedge-rag-server/${RAG_SERVER_BRANCH}"

prepare() {

  setup_apt_build_env

  # This function is for debugging purpose if you have your own keys. GH workflow does not need it.
  #import_gpg_keys

  echo "Resetting build workspace at ${SRC_DIR}..."
  rm -rf "$SRC_DIR"
  mkdir -p "$SRC_DIR"

  echo "Staging source tarball..."
  # Prefer the workflow-staged tarball (release-artifacts/) so simulate_tag
  # branch tests work; otherwise download the published release asset by tag.
  if [ -f "${ARTIFACT_DIR}/rag-server.tar.gz" ]; then
    cp "${ARTIFACT_DIR}/rag-server.tar.gz" "${BUILD_DIR}/rag-server.tar.gz"
  else
    wget -q "${RELEASE_URL}/pgedge-rag-server_${TAG_VERSION}_Linux_${ARCH}.tar.gz" -O "${BUILD_DIR}/rag-server.tar.gz"
  fi
  tar -C "$SRC_DIR" -xzf "${BUILD_DIR}/rag-server.tar.gz"

  echo "Moving Debian packaging into source directory..."
  cp -rp "${CWD}/${COMPONENT_NAME}/deb/debian" "$SRC_DIR/"
  cp "${COMPONENT_NAME}"/common/pgedge-rag-server.* "$SRC_DIR/debian/"

  echo "Staging LICENCE.md..."
  # GoReleaser archive globs LICENSE* and so omits the repo's LICENCE.md.
  if [ -f "${ARTIFACT_DIR}/LICENCE.md" ]; then
    cp "${ARTIFACT_DIR}/LICENCE.md" "$SRC_DIR/"
  else
    wget -q "${RAW_URL}/LICENCE.md" -O "$SRC_DIR/LICENCE.md"
  fi

  echo "Installing build dependencies..."
  cd "$SRC_DIR"
  sudo apt-get update
  sudo apt-get build-dep -y .
}

build() {

  cd "$SRC_DIR"
  echo "Building Debian package..."
  DISTRO=$(lsb_release -cs)
  rm -f debian/changelog
cat > debian/changelog <<EOF
pgedge-rag-server (${RAG_SERVER_VERSION}-${RAG_SERVER_BUILDNUM}.${DISTRO}) ${DISTRO}; urgency=medium

  * Update pgedge-rag-server package.

 -- pgEdge Build Team <support@pgedge.com>  $(date -R)
EOF

  dpkg-buildpackage -us -uc -b
}

post_build() {
  echo "Copying .deb packages to output..."
  sudo mkdir -p "/output"
  # Rename .ddeb files to .deb files
  rename_ddeb_packages $BUILD_DIR
  sudo cp "$BUILD_DIR"/*.deb "/output" || echo "No .deb packages found."
}
