%global debug_package %{nil}
%global sname pgedge-rag-server

Name:           %{sname}
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
install -D -m 0755 %{sname} %{buildroot}/usr/bin/%{sname}
install -D -m 0644 %{SOURCE1} %{buildroot}%{_unitdir}/%{sname}.service
install -D -m 0644 %{SOURCE2} %{buildroot}%{_tmpfilesdir}/%{sname}.conf
install -D -m 0644 %{SOURCE3} %{buildroot}%{_sysconfdir}/logrotate.d/%{sname}
mkdir -p %{buildroot}%{_sysconfdir}/pgedge
install -D -m 0644 %{SOURCE4} %{buildroot}%{_sysconfdir}/pgedge/%{sname}.yaml
install -d %{buildroot}/var/lib/pgedge/rag-server
install -d %{buildroot}/var/log/pgedge/rag-server
mkdir -p %{buildroot}%{_datadir}/%{sname}
install -p -m 0644 %{_builddir}/%{sname}-sbom.json %{buildroot}%{_datadir}/%{sname}/%{sname}-sbom.json
install -p -m 0644 %{_builddir}/%{sname}-sbom.json.asc %{buildroot}%{_datadir}/%{sname}/%{sname}-sbom.json.asc

%pre
# Ensure pgedge user/group exists
getent group pgedge >/dev/null || groupadd -r pgedge
getent passwd pgedge >/dev/null || \
    useradd -r -g pgedge -d /var/lib/pgedge -s /sbin/nologin \
    -c "pgEdge Services" pgedge
exit 0

%post
%systemd_post %{sname}.service
%tmpfiles_create %{_tmpfilesdir}/%{sname}.conf

%preun
%systemd_preun %{sname}.service

%postun
%systemd_postun_with_restart %{sname}.service

%files
%license LICENCE.md
%doc README.md
%{_bindir}/%{sname}
%{_unitdir}/%{sname}.service
%{_tmpfilesdir}/%{sname}.conf
%config(noreplace) %{_sysconfdir}/logrotate.d/%{sname}
%config(noreplace) %{_sysconfdir}/pgedge/%{sname}.yaml
%dir %attr(0755,pgedge,pgedge) /var/lib/pgedge
%dir %attr(0755,pgedge,pgedge) /var/lib/pgedge/rag-server
%dir %attr(0755,pgedge,pgedge) /var/log/pgedge
%dir %attr(0755,pgedge,pgedge) /var/log/pgedge/rag-server
%{_datadir}/%{sname}/%{sname}-sbom.json
%{_datadir}/%{sname}/%{sname}-sbom.json.asc

%changelog
* Mon Jun 16 2026 pgEdge Build Team <support@pgedge.com> - 1.0.0
- Move packaging in-repo (built from this repo's release.yml).
* Sat Apr 04 2026 Muhammad Aqeel <muhammad.aqeel@pgedge.com> - 1.0.0
- Update RPM package of pgedge-rag-server
* Tue Jan 27 2026 Muhammad Aqeel <muhammad.aqeel@pgedge.com> - 1.0.0-beta3
- Update RPM package of pgedge-rag-server
* Mon Dec 15 2025 Muhammad Aqeel <muhammad.aqeel@pgedge.com> - 1.0.0-beta1
- Initial RPM package of pgedge-rag-server
