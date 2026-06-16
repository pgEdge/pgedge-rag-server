#!/bin/bash
set -euo pipefail

RHEL="$(rpm --eval %rhel)"
ARCH=$(uname -m)
if [ "$ARCH" = "aarch64" ]; then
  ARCH="arm64"
fi

# Release assets are named with the full tag version (e.g. 1.0.0-beta3);
# RAG_SERVER_VERSION is the suffix-stripped value the spec expects in SOURCES.
TAG_VERSION="${RAG_SERVER_BRANCH#v}"
ARTIFACT_DIR="${ARTIFACT_DIR:-$(pwd)/release-artifacts}"
RELEASE_URL="https://github.com/pgEdge/pgedge-rag-server/releases/download/${RAG_SERVER_BRANCH}"
RAW_URL="https://raw.githubusercontent.com/pgEdge/pgedge-rag-server/${RAG_SERVER_BRANCH}"

prepare() {
  setup_dnf_build_env

  echo "Copying packaging files..."
  cp "${COMPONENT_NAME}/rpm/pgedge-rag-server.spec" ~/rpmbuild/SPECS/

  echo "Staging source tarball into SOURCES..."
  # Prefer the workflow-staged tarball (release-artifacts/) so simulate_tag
  # branch tests work and the package cell doesn't depend on the GH release;
  # fall back to downloading the published release asset by tag.
  local dest=~/rpmbuild/SOURCES/pgedge-rag-server_${RAG_SERVER_VERSION}_Linux_${ARCH}.tar.gz
  if [ -f "${ARTIFACT_DIR}/rag-server.tar.gz" ]; then
    cp "${ARTIFACT_DIR}/rag-server.tar.gz" "${dest}"
  else
    wget -q "${RELEASE_URL}/pgedge-rag-server_${TAG_VERSION}_Linux_${ARCH}.tar.gz" -O "${dest}"
  fi

  echo "Staging LICENCE.md..."
  # The GoReleaser archive globs LICENSE* and so omits the repo's LICENCE.md,
  # so it is staged separately (from the repo checkout, or fetched by tag).
  if [ -f "${ARTIFACT_DIR}/LICENCE.md" ]; then
    cp "${ARTIFACT_DIR}/LICENCE.md" ~/rpmbuild/SOURCES/
  else
    wget -q "${RAW_URL}/LICENCE.md" -O ~/rpmbuild/SOURCES/LICENCE.md
  fi

  cp "${COMPONENT_NAME}"/common/pgedge-rag-server.* ~/rpmbuild/SOURCES/

  # This function is for debugging purpose if you have your own keys. GH workflow does not need it.
  #import_gpg_keys

  echo "🔧 Installing RPM build dependencies..."
  dnf builddep -y \
    --define "rag_server_version ${RAG_SERVER_VERSION}" \
    --define "rag_server_buildnum ${RAG_SERVER_BUILDNUM}" \
    --define "arch ${ARCH}" \
    ~/rpmbuild/SPECS/pgedge-rag-server.spec
}

build() {
  echo "Building RPM and SRPM..."
  QA_RPATHS=$(( 0xffff )) rpmbuild -ba ~/rpmbuild/SPECS/pgedge-rag-server.spec \
    --define "rag_server_version ${RAG_SERVER_VERSION}" \
    --define "rag_server_buildnum ${RAG_SERVER_BUILDNUM}" \
    --define "arch ${ARCH}"
}

post_build() {
  echo "📤 Copying built RPMs to /output..."
  mkdir -p /output
  cp -v ~/rpmbuild/RPMS/*/*.rpm /output/ || echo "No binary RPMs found"
  cp -v ~/rpmbuild/SRPMS/*.src.rpm /output/ || echo "No SRPM found"

  sign_rpms /output/*.rpm
  validate_signatures /output/*.rpm
}
