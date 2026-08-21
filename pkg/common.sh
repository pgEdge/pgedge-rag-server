#!/usr/bin/env bash
# common.sh - packaging environment for pgedge-rag-server.
#
# Sourced by pkg/scripts/build.sh (via the common/build.sh bridge wrapper)
# before common-functions.sh. Sets the version variables consumed by
# build-rpm.sh / build-deb.sh and the RPM spec / debian rules.

export RAG_SERVER_REPO="https://github.com/pgEdge/pgedge-rag-server.git"
# Full tag (e.g. v2.0.0-beta1) — GoReleaser archives are named after this.
export RAG_SERVER_BRANCH="${COMPONENT_BRANCH:-v2.0.0}"
# Upstream version, suffix-stripped (e.g. 2.0.0) — used in spec/SOURCES names.
export RAG_SERVER_VERSION=${COMPONENT_VERSION:-2.0.0}
export RAG_SERVER_BUILDNUM=${COMPONENT_BUILDNUM:-1}

export REPO_TYPE="${REPO_TYPE:-daily}"

# Everything that must differ between concurrently-installed majors derives
# from this. Set before the '~beta' rewrite below, which mutates the version.
export RAG_SERVER_MAJOR="${RAG_SERVER_VERSION%%.*}"
export RAG_SERVER_PKGNAME="pgedge-rag-server${RAG_SERVER_MAJOR}"
export RAG_SERVER_INSTALL_ROOT="/usr/pgedge/rag-server${RAG_SERVER_MAJOR}"

# DEB only: move a pre-release pretag (e.g. BUILDNUM='beta3_1') into the
# upstream VERSION with a leading '~' (2.0.0~beta3, BUILDNUM=1) so '~' sorts
# pre-releases BELOW stable in dpkg/reprepro. Downloads use the tag
# (RAG_SERVER_BRANCH), not VERSION, so this never affects the source URL.
if command -v apt-get &>/dev/null; then
    if [[ "$RAG_SERVER_BUILDNUM" == *_* ]]; then
        RAG_SERVER_PRETAG="${RAG_SERVER_BUILDNUM%%_*}"
        export RAG_SERVER_VERSION="${RAG_SERVER_VERSION}~${RAG_SERVER_PRETAG}"
        RAG_SERVER_BUILDNUM="${RAG_SERVER_BUILDNUM#*_}"
    fi
fi

# Substitute the packaging placeholders in the fragments staged under <dir>.
expand_pkg_templates() {
    local dir="$1" f
    for f in "$dir"/pgedge-rag-server.*; do
        [ -f "$f" ] || continue
        sed -i \
            -e "s|@MAJOR@|${RAG_SERVER_MAJOR}|g" \
            -e "s|@PKGNAME@|${RAG_SERVER_PKGNAME}|g" \
            -e "s|@INSTALLROOT@|${RAG_SERVER_INSTALL_ROOT}|g" \
            "$f"
    done
}
