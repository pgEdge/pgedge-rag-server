#!/usr/bin/env bash
# common.sh - packaging environment for pgedge-rag-server.
#
# Sourced by pkg/scripts/build.sh (via the common/build.sh bridge wrapper)
# before common-functions.sh. Sets the version variables consumed by
# build-rpm.sh / build-deb.sh and the RPM spec / debian rules.

export RAG_SERVER_REPO="https://github.com/pgEdge/pgedge-rag-server.git"
# Full tag (e.g. v1.0.0-beta3) — GoReleaser archives are named after this.
export RAG_SERVER_BRANCH="${COMPONENT_BRANCH:-v1.0.0}"
# Upstream version, suffix-stripped (e.g. 1.0.0) — used in spec/SOURCES names.
export RAG_SERVER_VERSION=${COMPONENT_VERSION:-1.0.0}
export RAG_SERVER_BUILDNUM=${COMPONENT_BUILDNUM:-1}

export REPO_TYPE="${REPO_TYPE:-daily}"

# DEB only: move a pre-release pretag (e.g. BUILDNUM='beta3_1') into the
# upstream VERSION with a leading '~' (1.0.0~beta3, BUILDNUM=1) so '~' sorts
# pre-releases BELOW stable in dpkg/reprepro. Downloads use the tag
# (RAG_SERVER_BRANCH), not VERSION, so this never affects the source URL.
if command -v apt-get &>/dev/null; then
    if [[ "$RAG_SERVER_BUILDNUM" == *_* ]]; then
        RAG_SERVER_PRETAG="${RAG_SERVER_BUILDNUM%%_*}"
        export RAG_SERVER_VERSION="${RAG_SERVER_VERSION}~${RAG_SERVER_PRETAG}"
        RAG_SERVER_BUILDNUM="${RAG_SERVER_BUILDNUM#*_}"
    fi
fi
