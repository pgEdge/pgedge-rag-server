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

# One port per major, so 1.x (8080) and 2.x (8081) can run at the same time.
export RAG_SERVER_PORT="${RAG_SERVER_PORT:-$((8079 + RAG_SERVER_MAJOR))}"

# The repo's sample config: the single source for the shipped one.
export RAG_SERVER_SOURCE_YAML="${RAG_SERVER_SOURCE_YAML:-${COMPONENT_DIR:-$(pwd)/pkg}/../pgedge-rag-server.yaml}"

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

# Write the packaged config to <dest>, adjusted from the repo's sample for a
# native install. The sample lives outside pkg/ and changes freely, so awk
# counts its edits and fails the build rather than shipping a stale default.
stage_packaged_yaml() {
    local dest="$1"
    local src="${RAG_SERVER_SOURCE_YAML}"
    local tmp="${dest}.tmp"

    if [ ! -f "$src" ]; then
        echo "ERROR: sample config not found: $src" >&2
        return 1
    fi

    {
        cat <<EOF
# pgEdge RAG Server ${RAG_SERVER_MAJOR}.x configuration.
#
# Installed as /etc/pgedge/${RAG_SERVER_PKGNAME}.yaml. Edits survive package
# upgrades and are applied without a restart. The version in the file name,
# and the port below, are what let this run alongside another major.
EOF
        awk -v port="${RAG_SERVER_PORT}" -v src="$src" '
            BEGIN { header = 1; ports = 0; hosts = 0; keys = 0 }
            header && /^[[:space:]]*(#.*)?$/ { next }
            { header = 0 }
            /^[[:space:]]*port:[[:space:]]*8080[[:space:]]*$/ {
                sub(/8080/, port); ports++
            }
            /^[[:space:]]*host:[[:space:]]*"postgres"[[:space:]]*$/ {
                sub(/"postgres"/, "\"localhost\""); hosts++
            }
            # Ahead of pipelines:, and above its comment rather than under it.
            keys == 0 && (/^# Define one or more RAG pipelines/ || /^pipelines:/) {
                print "# API key locations (optional). Left unset, each key is read from the"
                print "# provider environment variable, then /var/lib/pgedge/.<provider>-api-key."
                print "# Uncomment for explicit paths, readable only by pgedge."
                print "#"
                print "# api_keys:"
                print "#     openai: \"/etc/pgedge/keys/openai.key\""
                print "#     anthropic: \"/etc/pgedge/keys/anthropic.key\""
                print "#     voyage: \"/etc/pgedge/keys/voyage.key\""
                print "#     gemini: \"/etc/pgedge/keys/gemini.key\""
                print ""
                keys++
            }
            { print }
            END {
                if (ports != 1 || hosts != 1 || keys != 1) {
                    printf("ERROR: %s no longer matches what stage_packaged_yaml expects\n", src) > "/dev/stderr"
                    printf("       (rewrote %d port and %d host lines and inserted %d api_keys\n", ports, hosts, keys) > "/dev/stderr"
                    printf("       blocks, expected 1 each);\n") > "/dev/stderr"
                    printf("       update the patterns in pkg/common.sh to match it.\n") > "/dev/stderr"
                    exit 1
                }
            }
        ' "$src"
    } > "$tmp" || { rm -f "$tmp"; return 1; }

    mv "$tmp" "$dest"
}
