# Side-by-side packaging for rag-server 1.x and 2.x

Goal: allow `pgedge-rag-server` 1.0.0 and 2.0.0 to be installed and run
concurrently on the same host, from both DNF and APT repositories.

Decisions already taken (do not revisit):

- Package renaming is approved: `pgedge-rag-server2`.
- 2.x installs its binary and SBOMs into a versioned directory,
  `/usr/pgedge/rag-server2/`, following the PGDG `/usr/pgsql-16/`
  pattern. It holds `bin/` and `sbom/`.
- The configuration file stays under `/etc/pgedge`, where a config file
  belongs, but is renamed to `pgedge-rag-server2.yaml` so it does not
  collide with 1.x's `pgedge-rag-server.yaml`.
- **No `/usr/bin` entry for 2.x** — no binary, no symlink, no
  alternatives. The binary is invoked by its full path.
- `/usr/pgedge` and the install root stay `root:root`; only the
  `/var` directories are owned by `pgedge`.
- Unit, tmpfiles, logrotate, state and log paths all carry the major.
- The shipped default port changes for 2.x.
- **1.0.0 will not be re-released.** Everything here applies to 2.x and
  later only; the published 1.0.0 package is left exactly as it is.

Files in scope: `pkg/common.sh`, `pkg/build-rpm.sh`, `pkg/build-deb.sh`,
`pkg/rpm/pgedge-rag-server.spec`, `pkg/deb/debian/*`, `pkg/common/*`,
`internal/config/loader.go`, `cmd/pgedge-rag-server/main.go`, docs.

## 1. Consequence of not re-releasing 1.0.0

1.0.0 keeps the name `pgedge-rag-server` and keeps every unversioned
path it owns today:

```
/usr/bin/pgedge-rag-server
/usr/lib/systemd/system/pgedge-rag-server.service
/usr/lib/tmpfiles.d/pgedge-rag-server.conf
/etc/logrotate.d/pgedge-rag-server
/etc/pgedge/pgedge-rag-server.yaml
/usr/share/pgedge-rag-server/pgedge-rag-server-sbom.json{,.asc}
/var/lib/pgedge/rag-server
/var/log/pgedge/rag-server
```

2.x must avoid all of them. Dropping the `/usr/bin` entry makes this
mostly automatic — the whole payload moves under
`/usr/pgedge/rag-server2/` except the unit, tmpfiles, logrotate and
`/var` directories, all of which are suffixed.

Two metadata rules still apply:

- 2.x must **not** carry `Obsoletes:`/`Provides: pgedge-rag-server`,
  `Conflicts:`, `Replaces:` or `Breaks:` against it. Any of those
  removes or blocks the 1.0.0 install.
- The only paths the two packages may share are the parent directories
  `/var/lib/pgedge` and `/var/log/pgedge` (see §3).

## 2. Target layout for 2.x

Naming: major-version suffix with no separator, matching the PGDG
convention (`postgresql16`) and avoiding a `-` in an RPM `Name`. One
suffix form everywhere, so there is nothing to remember.

```
/usr/pgedge/rag-server2/                      <- install root
    bin/pgedge-rag-server                     <- binary keeps its plain name
    sbom/pgedge-rag-server-sbom.json
    sbom/pgedge-rag-server-sbom.json.asc
/etc/pgedge/pgedge-rag-server2.yaml           <- config (noreplace / conffile)
/usr/lib/systemd/system/pgedge-rag-server2.service
/usr/lib/tmpfiles.d/pgedge-rag-server2.conf
/etc/logrotate.d/pgedge-rag-server2
/var/lib/pgedge/rag-server2/
/var/log/pgedge/rag-server2/
```

Notes on the install root:

- Hardcode the literal `/usr/pgedge/rag-server2`; do **not** use
  `%{_libdir}` or any macro that varies by arch. The unit file and the
  config-discovery logic both embed this path, so it must be identical
  on RPM and DEB and on x86_64 and arm64.
- `/usr/pgedge` and the install root are owned by **`root:root`, mode
  `0755`** — the ordinary ownership for anything under `/usr`. Nothing
  in the service writes below `/usr`: it reads the binary, the config
  and the SBOMs, and writes only to `/var/lib/pgedge/rag-server2` and
  `/var/log/pgedge/rag-server2`, both of which the tmpfiles and
  maintainer scripts already hand to `pgedge`. Keeping `/usr` root-owned
  also means the service user cannot replace its own binary or config,
  so a compromised server has no write-back path to persist itself.
- 1.0.0 does not own `/usr/pgedge`, so there is no conflict with 1.x.
  A future `rag-server3` will share the directory with `rag-server2`,
  and `root:root 0755` is the stable default both will naturally agree
  on — but the attributes must still be identical in both specs, so fix
  them now. If more components land under `/usr/pgedge/`, move the
  directory into a small `pgedge-filesystem` package that every major
  depends on.
- Root ownership also keeps RPM and DEB identical here.
  `pkg/deb/debian/control:8` sets `Rules-Requires-Root: no`, so
  `dpkg-deb` builds unprivileged and records every path in the archive
  as `root:root` — non-root ownership cannot be shipped in a `.deb` at
  all, which is why the existing `preinst` resorts to `mkdir -p` +
  `chown -R` for the `/var` dirs. With a root-owned install tree the
  DEB needs no ownership fix-up in the maintainer scripts and the RPM
  needs no `%attr` override; both families get the same result from the
  default. Keep `Rules-Requires-Root: no`.
- If protecting the config's secrets matters — it holds a plaintext
  database `password` and API-key file paths, and is currently
  world-readable at `0644` — the place to spend a non-root ownership is
  the file, not the directory: `root:pgedge` mode `0640`. That still
  needs a `chgrp`/`chmod` in the DEB `postinst` for the reason above, so
  treat it as a separate decision from this one. Flagging it because the
  relocation is a natural moment to tighten it; the plan below keeps the
  existing `0644` and does not change it.
- Because there is no `/usr/bin` entry, 2.x is not on `$PATH`. Users run
  `/usr/pgedge/rag-server2/bin/pgedge-rag-server`. Do **not** ship a
  `profile.d` snippet — with two majors installed, whichever package
  wrote its snippet last would silently decide which version a bare
  `pgedge-rag-server` resolves to. Document the full path instead; this
  is what PGDG does.

**On the configuration file's location.** Keeping it under `/etc/pgedge`
rather than inside the install root is what FHS and Debian policy
§10.7.2 ask for, and it avoids the lintian
`file-in-usr-marked-as-conffile` complaint a conffile under `/usr` would
draw. `/etc/pgedge` is left unowned by the package, matching what the
1.0.0 package does, so the two majors' `%files` lists cannot disagree
about its attributes.

## 3. Changes by file

### `pkg/common.sh`

Derive the suffix once, before the DEB `~beta` rewrite (which mutates
`RAG_SERVER_VERSION`):

- `RAG_SERVER_MAJOR="${RAG_SERVER_VERSION%%.*}"`
- `RAG_SERVER_PKGNAME="pgedge-rag-server${RAG_SERVER_MAJOR}"`
- `RAG_SERVER_INSTALL_ROOT="/usr/pgedge/rag-server${RAG_SERVER_MAJOR}"`

Export all three. `COMPONENT_NAME` stays as-is — it addresses the `pkg/`
directory, not the package.

### `pkg/rpm/pgedge-rag-server.spec`

- Keep `%global sname pgedge-rag-server` as the *project* name (the
  release tarball and the binary inside it still use it). Add
  `%global pname pgedge-rag-server%{rag_server_major}` and
  `%global installroot /usr/pgedge/rag-server%{rag_server_major}`.
- `Name: %{pname}`. `Source0` is unchanged — it is the upstream release
  asset and is already version-scoped.
- `%install` becomes:
  - `install -D -m 0755 %{sname} %{buildroot}%{installroot}/bin/%{sname}`
  - `install -D -m 0644 %{SOURCE4} %{buildroot}%{_sysconfdir}/pgedge/%{pname}.yaml`
  - both SBOM files into `%{buildroot}%{installroot}/sbom/`
  - unit at `%{buildroot}%{_unitdir}/%{pname}.service`, tmpfiles at
    `%{buildroot}%{_tmpfilesdir}/%{pname}.conf`, logrotate at
    `%{buildroot}%{_sysconfdir}/logrotate.d/%{pname}`
  - `install -d` for `/var/lib/pgedge/rag-server%{rag_server_major}` and
    `/var/log/pgedge/rag-server%{rag_server_major}`
  - drop the `%{_bindir}`, `%{_sysconfdir}/pgedge` and
    `%{_datadir}/%{sname}` targets entirely — all three are 1.x paths.
- `%files` mirrors the above, plus `%dir /usr/pgedge`,
  `%dir %{installroot}` and its two subdirs (no `%attr` — the default
  `root:root 0755` is what we want), and
  `%config(noreplace) %{_sysconfdir}/pgedge/%{pname}.yaml`.
  For the shared parents keep
  `%dir %attr(0755,pgedge,pgedge) /var/lib/pgedge` and `/var/log/pgedge`
  with **byte-identical attributes to the 1.0.0 package** — rpm permits
  duplicate directory ownership only when the attributes match, and
  1.0.0 is frozen, so 2.x adapts to it. Confirm against the shipped
  1.0.0 rpm with `rpm -qp --dump` rather than assuming.
- `%post`/`%preun`/`%postun` scriptlets take `%{pname}.service`, and
  `%tmpfiles_create %{_tmpfilesdir}/%{pname}.conf`.
- `%pre` user/group creation stays unchanged — idempotent, safe to run
  from both packages.
- New `%changelog` entry describing the relocation.

### `pkg/deb/debian/control`

- `Source: pgedge-rag-server2`, `Package: pgedge-rag-server2`. The
  rename also moves `/usr/share/doc/<pkg>`, so `debian/docs` stops
  colliding on its own.
- No `Conflicts:`/`Replaces:`/`Breaks:`/`Provides:` against
  `pgedge-rag-server`.

### `pkg/deb/debian/rules`

- Parameterise: `RAG_SERVER_MAJOR ?= 2`,
  `PKG := pgedge-rag-server$(RAG_SERVER_MAJOR)`,
  `INSTALLROOT := /usr/pgedge/rag-server$(RAG_SERVER_MAJOR)`.
- `override_dh_auto_install` installs the binary and SBOMs into
  `debian/$(PKG)$(INSTALLROOT)/{bin,sbom}` and the config into
  `debian/$(PKG)/etc/pgedge/$(PKG).yaml`. The *source* filename stays
  `pgedge-rag-server`; only destinations are suffixed. No `usr/bin`
  install and no `dh_link`.
- SBOM generation is unchanged; only its destination moves to
  `$(INSTALLROOT)/sbom/`.
- tmpfiles / logrotate / unit destinations take the `$(PKG)` name.
- `override_dh_installsystemd: dh_installsystemd --name=$(PKG) --no-stop-on-upgrade`.
- Expect a lintian `dir-or-file-in-unusual-dir` style complaint for
  `/usr/pgedge`; add a `debian/source/lintian-overrides` entry if the
  build treats lintian output as fatal.
- `RAG_SERVER_MAJOR` must be exported into the `dpkg-buildpackage`
  environment by `build-deb.sh`.

### `pkg/deb/debian/` file renames

- `pgedge-rag-server.conffiles` is **deleted**, not renamed. With the
  config file back under `/etc`, `dh_installdeb` registers it as a
  conffile automatically — the same way it already handled the logrotate
  fragment, which was never declared. Keeping an explicit declaration
  listed the file twice in the built package's `conffiles`. This also
  removes a footgun: debhelper resolves `debian/<pkg>.conffiles` by
  binary package name, so a major bump that forgot to rename it would
  have silently dropped the marker and overwritten user edits on
  upgrade.
- `preinst`: create only `/var/lib/pgedge/rag-server2` and
  `/var/log/pgedge/rag-server2`; keep `/var/lib/pgedge` and the
  `pgedge` user/group creation as-is.
- `postinst`: unchanged apart from the tmpfiles filename —
  `systemd-tmpfiles --create /usr/lib/tmpfiles.d/pgedge-rag-server2.conf`.
  No `chown` is needed anywhere under `/usr`.

### `pkg/common/*`

These are copied verbatim into both build flavours, so the major has to
be baked in. Either rename them with the suffix and substitute an
`@MAJOR@`/`@INSTALLROOT@` placeholder at build time, or simply hardcode
`2` — the `pkg/` tree lives on the release branch, which already pins
the major, so hardcoding is fine and simpler.

- `pgedge-rag-server.service` → `pgedge-rag-server2.service`:
  - `WorkingDirectory=/var/lib/pgedge/rag-server2`
  - `ExecStart=/usr/pgedge/rag-server2/bin/pgedge-rag-server -config /etc/pgedge/pgedge-rag-server2.yaml`
  - `StandardOutput`/`StandardError` append to
    `/var/log/pgedge/rag-server2/pgedge-rag-server.log`
  - `WantedBy=multi-user.target` stays; the two units enable
    independently.
- `pgedge-rag-server.tmpfiles.conf`: both `d` lines point at the
  `rag-server2` dirs.
- `pgedge-rag-server.logrotate`: glob
  `/var/log/pgedge/rag-server2/*.log`, and the `postrotate`
  `systemctl kill -s HUP` must name `pgedge-rag-server2.service` — miss
  this and rotating 2.x's logs signals 1.x's service.
- `pgedge-rag-server.yaml`: set `port: 8081` so a dual install starts
  cleanly out of the box. Comment why, and note 1.x holds 8080.

### `pkg/build-rpm.sh`

- Pass `--define "rag_server_major ${RAG_SERVER_MAJOR}"` to both
  `dnf builddep` and `rpmbuild`.
- Keep the `SOURCES/` filenames unsuffixed
  (`pgedge-rag-server.service`, …) so the existing
  `cp "${COMPONENT_NAME}"/common/pgedge-rag-server.*` glob and the
  spec's `SourceN` lines keep matching; the suffix is applied at the
  `%install` destination.

### `pkg/build-deb.sh`

- The generated `debian/changelog` hardcodes the source name
  (`pgedge-rag-server (${RAG_SERVER_VERSION}-...)`). It must emit
  `${RAG_SERVER_PKGNAME}` or `dpkg-buildpackage` aborts on a
  source-name mismatch with `control`.
- `export RAG_SERVER_MAJOR` before `dpkg-buildpackage`.
- Suffixing `SRC_DIR` is cosmetic but worth it for readable build logs.

## 4. Config discovery in the 2.x binary

Current behaviour (`internal/config/loader.go`) searched, in order:
explicit `-config`, then the compile-time constant
`SystemConfigPath = /etc/pgedge/pgedge-rag-server.yaml`, then
`<dir of resolved executable>/pgedge-rag-server.yaml`.

The middle entry is **1.0.0's config file**, and it was checked before
anything else. A bare 2.x invocation on a host with 1.x installed would
start 2.x against 1.x's config — wrong port, wrong pipelines, possibly
wrong schema.

Because the packaged file now carries the major in its name, discovery
keys off that name rather than off a location:

1. explicit `-config` path
2. `$PGEDGE_RAG_SERVER_CONFIG`
3. `/etc/pgedge/pgedge-rag-server<major>.yaml` — the packaged file
4. `<exeDir>/pgedge-rag-server.yaml` — an extracted release archive, or
   a working copy during development
5. `/etc/pgedge/pgedge-rag-server.yaml` — legacy, last

Implementation notes:

- The major is derived from the build's own version string rather than
  hardcoded. `main` assigns `config.BuildVersion = version`, which
  ldflags already stamps, and `MajorVersion` takes the leading numeric
  component. Hardcoding the file name would mean a future major
  silently reading this one's config if someone forgot to bump a
  constant; deriving it makes that impossible.
- `MajorVersion` returns `""` for anything it cannot parse (`dev`,
  `v2.0.0`, an empty string), and the versioned search location is then
  skipped rather than degenerating into `pgedge-rag-server.yaml`, which
  would collide with the legacy name.
- Both `-config` and `$PGEDGE_RAG_SERVER_CONFIG` are errors when they
  name a file that does not exist, rather than falling through to the
  search — a deliberate path that silently resolves elsewhere is how
  you end up running against the wrong major's config.
- Keep the `EvalSymlinks` call on `os.Executable()`: it costs nothing
  and keeps location 4 correct for anyone who symlinks the binary onto
  their `$PATH`.
- Adding the versioned name **ahead of** `/etc/pgedge/...` is a
  behaviour change for anyone who has both files. Acceptable at a major
  boundary, but call it out in the release notes.
- Keep the error message listing every path searched — it is the main
  debugging aid when discovery picks the "wrong" file.

Also update:

- The search-order doc comments on `Load` and `FindConfigFile`.
- The `-config` help text in `cmd/pgedge-rag-server/main.go`.
- Tests: `FindConfigFile` had **no** existing coverage (nothing in
  `config_test.go` touched it). Cover precedence across all five
  locations, `MajorVersion` parsing, and the no-derivable-major case —
  overriding the executable-resolution and system-path seams so the
  tests never depend on the real `/etc`.

## 5. Repository and workflow

`release.yml` keys repo paths on
`${REPO_TYPE}/${COMPONENT_NAME}/${CV}/...`, so 1.x and 2.x metadata
already land in separate DNF/APT trees and cannot overwrite each other.
No workflow change is needed. The user-visible consequence is that
**both repo definitions must be enabled** to install the two majors
together — the install docs must say so.

Nothing in `pkg/scripts/build.sh` or `pkg/scripts/common-functions.sh`
hardcodes the package name, so they need no changes.

## 6. Documentation

`README.md`, `docs/usage.md`, `docs/quickstart.md`,
`docs/configuration.md` and `docs/docker.md` all reference
`/etc/pgedge/pgedge-rag-server.yaml`, the unit name and
`/usr/bin/pgedge-rag-server`. All need the 2.x paths, plus:

- an explicit note that 2.x is **not on `$PATH`** and is run as
  `/usr/pgedge/rag-server2/bin/pgedge-rag-server`;
- the renamed configuration file, `/etc/pgedge/pgedge-rag-server2.yaml`;
- a side-by-side install section: enabling both repos, the two package
  and unit names, 8080 vs 8081, and the fact that the bare
  `pgedge-rag-server` command belongs to 1.x;
- the new config search order in `docs/configuration.md`.

`make openapi` is not required — no API surface changes.

## 7. Verification

- `make test`, `make lint`, and `gofmt` for the `loader.go` and
  `main.go` changes.
- `dpkg -c` / `rpm -qlp --dump` the 2.x package against the published
  1.0.0 package and diff. The only shared entries must be
  `/var/lib/pgedge` and `/var/log/pgedge`, with identical mode, owner
  and group.
- After installing, `stat -c '%U:%G %a' /usr/pgedge /usr/pgedge/rag-server2`
  on both RPM and DEB and confirm both report `root:root 755`, and that
  the service still starts as `pgedge` and can write to its `/var`
  directories.
- Container matrix via the `pgedge-build-test` skill (almalinux:10 and
  ubuntu:noble): install 1.0.0 from the existing repo, then 2.x on top,
  and confirm the 1.x install is untouched — no file replaced, no
  service restarted, `rpm -V` / `dpkg -V` clean.
- Start both units; confirm 8080 and 8081 both answer and each reports
  its own version.
- Run `/usr/pgedge/rag-server2/bin/pgedge-rag-server` with no `-config`
  and assert it resolves `/etc/pgedge/pgedge-rag-server2.yaml` and
  **not** `/etc/pgedge/pgedge-rag-server.yaml`. This is the regression
  that matters most.
- Remove 2.x and confirm `/usr/pgedge` is left in a sane state (still
  present only if another major owns it) and that 1.x still runs with
  `/usr/bin/pgedge-rag-server` intact.
