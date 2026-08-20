%global debug_package %{nil}
# sname is the project name (tarball, binary, fragments); pname is the package
# name, carrying the major so 1.x and 2.x install together. installroot is
# hardcoded, not %%{_libdir}, which varies by arch.
%global sname pgedge-rag-server
%global pname pgedge-rag-server%{rag_server_major}
%global installroot /usr/pgedge/rag-server%{rag_server_major}

Name:           %{pname}
Version:        %{rag_server_version}
Release:        %{rag_server_buildnum}%{?dist}
Summary:        A simple API server for performing Retrieval-Augmented Generation (RAG) of text based on content from a PostgreSQL database using pgvector.
License:        PostgreSQL License
URL:            https://github.com/pgEdge/%{sname}
Source0:	%{sname}_%{version}_Linux_%{arch}.tar.gz

Source1:        %{sname}.service
Source2:        %{sname}.tmpfiles.conf
Source3:        %{sname}.logrotate
Source4:	%{sname}.yaml
Source5:        LICENCE.md

BuildRequires:  systemd
Requires(post): systemd
Requires(preun): systemd
Requires(postun): systemd
Requires:       systemd
Requires:       logrotate

# No Obsoletes/Provides/Conflicts against pgedge-rag-server: 1.0.0 ships under
# that name and must survive this package being installed.

%description
A simple API server for performing Retrieval-Augmented Generation (RAG) of text based on content from a PostgreSQL database using pgvector.

%prep
%setup -q -c -n %{sname}-%{version}
cp %{SOURCE5} .

%build
syft dir:%{_builddir} -o cyclonedx-json > %{_builddir}/%{sname}-sbom.json || exit 1

KEY_ID=$(gpg --list-secret-keys --with-colons | awk -F: '/^sec/{print $5}' | head -n 1); export KEY_ID
gpg --armor --detach-sign --local-user "$KEY_ID" --output %{_builddir}/%{sname}-sbom.json.asc %{_builddir}/%{sname}-sbom.json || exit 1

%install
# No %%{_bindir} entry: 1.x owns /usr/bin/pgedge-rag-server.
install -D -m 0755 %{sname} %{buildroot}%{installroot}/bin/%{sname}
install -D -m 0644 %{SOURCE4} %{buildroot}%{_sysconfdir}/pgedge/%{pname}.yaml
install -D -m 0644 %{_builddir}/%{sname}-sbom.json %{buildroot}%{installroot}/sbom/%{sname}-sbom.json
install -D -m 0644 %{_builddir}/%{sname}-sbom.json.asc %{buildroot}%{installroot}/sbom/%{sname}-sbom.json.asc
install -D -m 0644 %{SOURCE1} %{buildroot}%{_unitdir}/%{pname}.service
install -D -m 0644 %{SOURCE2} %{buildroot}%{_tmpfilesdir}/%{pname}.conf
install -D -m 0644 %{SOURCE3} %{buildroot}%{_sysconfdir}/logrotate.d/%{pname}
install -d %{buildroot}/var/lib/pgedge/rag-server%{rag_server_major}
install -d %{buildroot}/var/log/pgedge/rag-server%{rag_server_major}

%pre
# Ensure pgedge user/group exists
getent group pgedge >/dev/null || groupadd -r pgedge
getent passwd pgedge >/dev/null || \
    useradd -r -g pgedge -d /var/lib/pgedge -s /sbin/nologin \
    -c "pgEdge Services" pgedge
exit 0

%post
%systemd_post %{pname}.service
%tmpfiles_create %{_tmpfilesdir}/%{pname}.conf

%preun
%systemd_preun %{pname}.service

%postun
%systemd_postun_with_restart %{pname}.service

%files
%license LICENCE.md
%doc README.md
# root:root: nothing under /usr is written at runtime. /usr/pgedge is shared
# with future majors, so its attributes must not change.
%dir /usr/pgedge
%dir %{installroot}
%dir %{installroot}/bin
%dir %{installroot}/sbom
%{installroot}/bin/%{sname}
# /etc/pgedge is left unowned, matching the 1.0.0 package.
%config(noreplace) %{_sysconfdir}/pgedge/%{pname}.yaml
%{installroot}/sbom/%{sname}-sbom.json
%{installroot}/sbom/%{sname}-sbom.json.asc
%{_unitdir}/%{pname}.service
%{_tmpfilesdir}/%{pname}.conf
%config(noreplace) %{_sysconfdir}/logrotate.d/%{pname}
# The only paths shared with 1.0.0. rpm allows duplicate directory ownership
# only when the attributes match, so these must stay identical to its spec.
%dir %attr(0755,pgedge,pgedge) /var/lib/pgedge
%dir %attr(0755,pgedge,pgedge) /var/lib/pgedge/rag-server%{rag_server_major}
%dir %attr(0755,pgedge,pgedge) /var/log/pgedge
%dir %attr(0755,pgedge,pgedge) /var/log/pgedge/rag-server%{rag_server_major}

%changelog
* Thu Aug 20 2026 pgEdge Build Team <support@pgedge.com> - 2.0.0
- Rename the package to pgedge-rag-server2, relocate the binary and SBOMs to
  /usr/pgedge/rag-server2 and the configuration file to
  /etc/pgedge/pgedge-rag-server2.yaml, so 1.x and 2.x can be installed side
  by side.
* Tue Jun 16 2026 pgEdge Build Team <support@pgedge.com> - 1.0.0
- Move packaging in-repo (built from this repo's release.yml).
* Sat Apr 04 2026 Muhammad Aqeel <muhammad.aqeel@pgedge.com> - 1.0.0
- Update RPM package of pgedge-rag-server
* Tue Jan 27 2026 Muhammad Aqeel <muhammad.aqeel@pgedge.com> - 1.0.0-beta3
- Update RPM package of pgedge-rag-server
* Mon Dec 15 2025 Muhammad Aqeel <muhammad.aqeel@pgedge.com> - 1.0.0-beta1
- Initial RPM package of pgedge-rag-server
