#!/usr/bin/env python3
"""Evidence check for the temporary PEP chain check (full or preview). Validation harness only.

Run by the chain workflows' assert jobs; also run offline against fixtures. Usage:

    verify_chain.py <full|preview> <evidence-dir>

The evidence directory holds this run's downloaded PEP artifacts (pep-certification-*,
pep-invocation-plan-*, pep-capture-evidence-*, pep-summary-*). Run metadata, the attempt's jobs
and their logs come from the GitHub API, or, when VERIFY_FIXTURES names a directory, from
run.json, jobs.json and logs/<job id>.log there. NEEDS_JSON is the assert job's toJSON(needs).
SELF_JOB_ID is the assert job's own id (job.check_run_id): that job is still running, so it is
the one job whose log is not read; every other job must have completed with a readable log.
Exit status 0 only if the evidence matches the mode's expectations.

The adapter is called through the branch reference @v1_STABLE. The reference and the commit it
resolved to are checked separately: GitHub records the adapter as `...@v1_STABLE` (ref
refs/heads/v1_STABLE) and each nested PEP workflow as `...@<commit>`; all must be STABLE_SHA, as
must PEP's own provenance records and its checkouts.

What the runs are: the packages are the already-published pgedge-rag-server2 2.0.0 release
packages, downloaded and checksum-verified, and the receipts are recorded in the run. Nothing is
built or published, and no original build provenance is established. In the full run the
`success` publication inputs are synthetic.
"""
import http.client
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path
from urllib.parse import urlsplit

STABLE_REF = "v1_STABLE"
STABLE_SHA = "9b3c9e964487568a7b3f3a8f0a3c68000fe0d943"   # v1_STABLE after PEP #38 merged
PEP_WF = "pgEdge/pgedge-pep-test/.github/workflows/"
NESTED = ("pep-capture.yml", "pep-certify.yml", "pep-integration.yml")
HEAD_LINE = re.compile(r"HEAD is now at ([0-9a-f]{7,40}) ")
NAMES = ("PGEDGE_REPO_CLIENT_CRT", "PGEDGE_REPO_CLIENT_KEY")
CELLS = {"rpm": "pepcell.v1.rpm.el-9.amd64.pkg", "deb": "pepcell.v1.deb.bookworm.amd64.pkg"}
SHA256 = {"rpm": "01717af9c7ba89cebba85ec9399d9de6b144cf0a8183ba2d38cbb0e7d21bc617",
          "deb": "eba1a2c463ad2ad8ed392e953387a80353103fff144bb72b377d22dc51fe0754"}
PG = ("16", "17", "18")
LEGS = {(fam, pg) for fam in ("rpm", "deb") for pg in PG}
PACKAGE = "pgedge-rag-server2"
IDENTITY = {"l1": "proven", "l2a": "proven", "l2b": "not_attempted"}
EXPECT = {
    "full": dict(outputs={"certification_state": "pass", "execution_status": "completed",
                          "test_verdict": "pass", "coverage_status": "complete"},
                 counts={"expected_invocations": 6, "matched": 6, "completed": 6, "pass": 6, "fail": 0,
                         "incomplete": 0, "infra_failure": 0, "not_run": 0, "preview": 0, "missing": 0,
                         "unexpected_results": 0, "planner_gaps": 0},
                 suffix=" / full test (install and product tests)", assert_job="assert chain evidence (full)"),
    "preview": dict(outputs={"certification_state": "preview", "execution_status": "preview",
                             "test_verdict": "not_run", "coverage_status": "partial"},
                    counts={"expected_invocations": 6, "matched": 6, "completed": 0, "pass": 0, "fail": 0,
                            "incomplete": 0, "infra_failure": 0, "not_run": 6, "preview": 6, "missing": 0,
                            "unexpected_results": 0, "planner_gaps": 0},
                    suffix=" / preview (no install, no product tests)", assert_job="assert chain evidence (preview)"),
}

CONTROL = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-_]|[\x00-\x08\x0b-\x1f\x7f-\x9f]")
ENV_LINE = re.compile(r"^\S+\s+(PGEDGE_REPO_CLIENT_(?:CRT|KEY)):(?: (.*))?$")


class _HttpsOnlyRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        if urlsplit(newurl).scheme != "https":
            raise urllib.error.URLError("refused non-HTTPS redirect")
        return super().redirect_request(req, fp, code, msg, headers, newurl)


class Live:
    """The GitHub API; job logs over HTTPS with the token as an unredirected header."""

    def __init__(self, env):
        self.repo, self.run_id, self.att = env["GITHUB_REPOSITORY"], env["GITHUB_RUN_ID"], env["GITHUB_RUN_ATTEMPT"]
        self.token = env.get("GH_TOKEN", "")

    def _gh(self, path, tries=3):
        for i in range(tries):
            r = subprocess.run(["gh", "api", path], capture_output=True, text=True)
            if r.returncode == 0:
                return json.loads(r.stdout)
            if i + 1 < tries:
                time.sleep(15)
        raise RuntimeError("gh api %s failed: %s" % (path, r.stderr.strip()[:300]))

    def run(self):
        return self._gh("repos/%s/actions/runs/%s" % (self.repo, self.run_id))

    def jobs(self):
        return self._gh("repos/%s/actions/runs/%s/attempts/%s/jobs?per_page=100" % (self.repo, self.run_id, self.att))["jobs"]

    def log(self, jid, tries=4):
        req = urllib.request.Request("https://api.github.com/repos/%s/actions/jobs/%s/logs" % (self.repo, jid))
        req.add_unredirected_header("Authorization", "Bearer " + self.token)
        for i in range(tries):
            try:
                with urllib.request.build_opener(_HttpsOnlyRedirect).open(req, timeout=120) as r:
                    return r.read().decode("utf-8", "replace")
            except (OSError, ValueError, http.client.HTTPException) as e:
                err = e
            if i + 1 < tries:
                time.sleep(15)
        raise RuntimeError("job %s log: %s" % (jid, err))


class Fixtures:
    def __init__(self, d):
        self.d = Path(d)

    def run(self):
        return json.loads((self.d / "run.json").read_text())

    def jobs(self):
        return json.loads((self.d / "jobs.json").read_text())["jobs"]

    def log(self, jid):
        return (self.d / "logs" / ("%s.log" % jid)).read_text()


def step_groups(lines):
    groups, cur = [], None
    for line in lines:
        if "##[group]Run " in line:
            cur = [line]
            groups.append(cur)
        elif cur is not None:
            cur.append(line)
            if "##[endgroup]" in line:
                cur = None
    return groups


def pair_env(lines):
    out = []
    for line in lines:
        m = ENV_LINE.match(line)
        if m:
            out.append((m.group(1), (m.group(2) or "").strip()))
    return out


def framework_group(lines):
    found = [g for g in step_groups(lines) if any("pep_time_limit.py --limit-seconds" in l for l in g)]
    return found[0] if len(found) == 1 else None


def _one(evidence, prefix, filename):
    hits = sorted(Path(evidence).glob("%s-r*-a*/%s" % (prefix, filename)))
    if len(hits) != 1:
        raise ValueError("%d %s/%s found" % (len(hits), prefix, filename))
    return json.loads(hits[0].read_text())


def verify(mode, evidence, source, env):
    want = EXPECT[mode]
    out = (json.loads(env["NEEDS_JSON"]).get("certify") or {}).get("outputs") or {}
    problems, rows = [], []

    def check(ok, what):
        if not ok:
            problems.append(what)
        return ok

    # 1. The adapter's own outputs: every axis, not just the state.
    for k, v in want["outputs"].items():
        check(out.get(k) == v, "certify output %s=%r, expected %r" % (k, out.get(k), v))

    # 2. Capture accepted exactly the two receipts.
    try:
        cap = _one(evidence, "pep-capture-evidence", "capture-evidence.json")
        c = cap.get("counts") or {}
        check((c.get("planned_cells"), c.get("accepted_receipt_cells"), c.get("receipt_candidates")) == (2, 2, 2),
              "capture planned/accepted/candidates %r" % ((c.get("planned_cells"), c.get("accepted_receipt_cells"), c.get("receipt_candidates")),))
        check((c.get("absent_receipt_cells"), c.get("rejected_receipt_cells"), c.get("ambiguous_receipt_cells")) == (0, 0, 0),
              "capture absent/rejected/ambiguous %r" % ((c.get("absent_receipt_cells"), c.get("rejected_receipt_cells"), c.get("ambiguous_receipt_cells")),))
        check(sorted((x.get("cell_id"), x.get("verdict")) for x in cap.get("cells") or []) ==
              sorted((cid, "accepted") for cid in CELLS.values()), "capture cells %r" % cap.get("cells"))
        cp = cap.get("provenance") or {}
        check((cp.get("pep_implementation_ref"), cp.get("pep_resolved_sha")) == (STABLE_SHA, STABLE_SHA),
              "capture provenance %r" % ((cp.get("pep_implementation_ref"), cp.get("pep_resolved_sha")),))
    except (OSError, ValueError) as e:
        problems.append("capture evidence: %s" % e)

    # 3. The plan: six invocations from two cells, no gaps.
    try:
        plan = _one(evidence, "pep-invocation-plan", "invocation-plan.json")
        pc = plan.get("counts") or {}
        check(plan.get("plan_resolved") is True and plan.get("execution_mode") == mode,
              "plan resolved/mode %r" % ((plan.get("plan_resolved"), plan.get("execution_mode")),))
        check((pc.get("invocations"), pc.get("planned_cells"), pc.get("coverage_gaps")) == (6, 2, 0),
              "plan invocations/cells/gaps %r" % ((pc.get("invocations"), pc.get("planned_cells"), pc.get("coverage_gaps")),))
        check(pc.get("gaps_by_scope") == {"cell": 0, "member": 0, "target": 0}, "plan gaps_by_scope %r" % pc.get("gaps_by_scope"))
        check(not plan.get("coverage_gaps"), "plan coverage_gaps %r" % plan.get("coverage_gaps"))
    except (OSError, ValueError) as e:
        problems.append("invocation plan: %s" % e)

    # 4. The certification result and every leg in it.
    try:
        res = _one(evidence, "pep-certification", "cert-result.json")
        dec = _one(evidence, "pep-certification", "cert-decision.json")
        check(dec.get("certification_state") == want["outputs"]["certification_state"], "decision state %r" % dec.get("certification_state"))
        for k in ("execution_status", "test_verdict", "coverage_status"):
            check(res.get(k) == want["outputs"][k], "result %s=%r" % (k, res.get(k)))
        check(res.get("result_resolved") is True, "result not resolved")
        rp = res.get("provenance") or {}
        check((rp.get("pep_requested_ref"), rp.get("pep_resolved_sha")) == (STABLE_SHA, STABLE_SHA),
              "result provenance %r" % ((rp.get("pep_requested_ref"), rp.get("pep_resolved_sha")),))
        check(not res.get("coverage_gaps"), "result coverage_gaps %r" % res.get("coverage_gaps"))
        check(not res.get("unexpected_results"), "unexpected results %r" % res.get("unexpected_results"))
        rc = res.get("counts") or {}
        for k, v in want["counts"].items():
            check(rc.get(k) == v, "result count %s=%r, expected %r" % (k, rc.get(k), v))
        legs = res.get("legs") or []
        keys = [((l.get("planned_invocation") or {}).get("family"), (l.get("planned_invocation") or {}).get("pg_major")) for l in legs]
        check(len(keys) == 6 and set(keys) == LEGS, "legs %r, expected RPM/DEB x PG16/17/18 once each" % sorted(keys))
        for leg in legs:
            pi = leg.get("planned_invocation") or {}
            fam, pg = pi.get("family"), pi.get("pg_major")
            tag = "leg %s pg%s: " % (fam, pg)
            pkg = pi.get("package") or {}
            check(pi.get("source_cell_id") == CELLS.get(fam), tag + "cell %r" % pi.get("source_cell_id"))
            check((pi.get("package_name"), pkg.get("name"), pkg.get("version"), pi.get("channel")) == (PACKAGE, PACKAGE, "2.0.0", "release"),
                  tag + "planned package %r" % ((pi.get("package_name"), pkg.get("name"), pkg.get("version"), pi.get("channel")),))
            check(pkg.get("sha256") == SHA256.get(fam), tag + "receipt sha256 %r" % pkg.get("sha256"))
            check((leg.get("provenance") or {}).get("pep_resolved_sha") == STABLE_SHA,
                  tag + "provenance %r" % (leg.get("provenance") or {}).get("pep_resolved_sha"))
            if mode == "full":
                check((leg.get("execution_status"), leg.get("test_verdict")) == ("completed", "pass"),
                      tag + "status/verdict %r" % ((leg.get("execution_status"), leg.get("test_verdict")),))
                check(leg.get("package_digest") == "match", tag + "package_digest %r" % leg.get("package_digest"))
                check(leg.get("installed_package_sha256") == SHA256.get(fam), tag + "installed sha256 %r" % leg.get("installed_package_sha256"))
                check(leg.get("identity_evidence") == IDENTITY, tag + "identity %r" % leg.get("identity_evidence"))
            else:
                check((leg.get("execution_status"), leg.get("test_verdict")) == ("preview", "not_run"),
                      tag + "status/verdict %r" % ((leg.get("execution_status"), leg.get("test_verdict")),))
                check(leg.get("installed_package_sha256") is None and (leg.get("counts") or {}).get("tests") == 0,
                      tag + "installed or tested in a preview")
    except (OSError, ValueError) as e:
        problems.append("certification result: %s" % e)

    # 5. Each leg's own uploaded evidence.
    summaries = sorted(Path(evidence).glob("pep-summary-*-r*-a*"))
    check(len(summaries) == 6, "%d leg summaries, expected 6" % len(summaries))
    for d in summaries:
        tag = d.name[:60] + ": "
        try:
            s = json.loads((d / "summary.json").read_text())
        except (OSError, ValueError) as e:
            problems.append(tag + "summary: %s" % e)
            continue
        fam = "rpm" if "-rpm-" in d.name else "deb" if "-deb-" in d.name else None
        check((s.get("provenance") or {}).get("pep_resolved_sha") == STABLE_SHA,
              tag + "summary provenance %r" % (s.get("provenance") or {}).get("pep_resolved_sha"))
        inst = d / "install-evidence.json"
        if mode == "full":
            check((s.get("execution_status"), s.get("test_verdict")) == ("completed", "pass"), tag + "summary %r" % ((s.get("execution_status"), s.get("test_verdict")),))
            check(s.get("installed_package_sha256") == SHA256.get(fam), tag + "summary installed sha256 %r" % s.get("installed_package_sha256"))
            try:
                ie = json.loads(inst.read_text())
                check(ie.get("installed") is True and ie.get("install_kind") == "pinned", tag + "not a pinned install")
                check(ie.get("installed_sha256") == SHA256.get(fam), tag + "installed sha256 %r" % ie.get("installed_sha256"))
                check((ie.get("target") or {}).get("package_name") == PACKAGE and (ie.get("target") or {}).get("family") == fam,
                      tag + "install target %r" % ie.get("target"))
            except (OSError, ValueError) as e:
                problems.append(tag + "install evidence: %s" % e)
        else:
            check(s.get("execution_status") == "preview", tag + "summary %r" % s.get("execution_status"))
            check((s.get("counts") or {}).get("tests", 0) == 0 and s.get("installed_package_sha256") is None, tag + "installed or tested in a preview")
            check(not inst.exists(), tag + "install evidence present in a preview")
        for f in (p for p in d.rglob("*") if p.is_file()):
            check(b"PRIVATE KEY" not in f.read_bytes(), tag + "%s contains 'PRIVATE KEY'" % f.relative_to(d))

    # 6. Which PEP code ran. The adapter's reference and the commit it resolved to are separate
    #    checks; the nested workflows GitHub records under the resolved commit, not the branch.
    try:
        refs = [(r.get("path"), r.get("ref"), r.get("sha")) for r in source.run().get("referenced_workflows") or []]
        adapter = [r for r in refs if (r[0] or "").split("@")[0] == PEP_WF + "pep-release-certify.yml"]
        nested = sorted((r for r in refs if r not in adapter), key=repr)
        check([r[:2] for r in adapter] == [(PEP_WF + "pep-release-certify.yml@" + STABLE_REF, "refs/heads/" + STABLE_REF)],
              "adapter not called through @%s: %r" % (STABLE_REF, adapter))
        check([r[2] for r in adapter] == [STABLE_SHA],
              "adapter resolved to %r, expected %s" % ([r[2] for r in adapter], STABLE_SHA))
        check(nested == sorted(((PEP_WF + n + "@" + STABLE_SHA, "refs/heads/" + STABLE_REF, STABLE_SHA) for n in NESTED), key=repr),
              "nested PEP workflows %r, expected %s at %s" % (nested, ", ".join(NESTED), STABLE_SHA))
        jobs = source.jobs()
    except (RuntimeError, OSError, ValueError) as e:
        problems.append("run metadata: %s" % e)
        jobs = []
    leg_jobs = [j for j in jobs if j.get("name", "").endswith(want["suffix"])]
    check(len(leg_jobs) == 6, "%d PEP test-leg jobs, expected 6" % len(leg_jobs))
    # This assert job has no log until it finishes, so it alone is skipped, and only when
    # SELF_JOB_ID names it exactly. Every other job is upstream of it: completed, log required.
    heads = set()
    own_id = env.get("SELF_JOB_ID", "")
    own = [j for j in jobs if str(j.get("id")) == own_id]
    if not check(own_id.isdigit() and len(own) == 1 and own[0].get("name") == want["assert_job"],
                 "own assertion job: SELF_JOB_ID %r does not name %r in this attempt" % (own_id, want["assert_job"])):
        own = []
    for j in jobs:
        if j in own:
            continue
        if not check(j.get("status") == "completed", "%s: required job not completed (%r)" % (j.get("name"), j.get("status"))):
            continue
        try:
            lines = CONTROL.sub("", source.log(j["id"])).splitlines()
        except (RuntimeError, OSError) as e:
            problems.append("%s: %s" % (j.get("name"), e))
            continue
        found = set(HEAD_LINE.findall("\n".join(lines)))
        heads |= found
        if j in leg_jobs:
            check(found, "%s: no PEP checkout found" % j.get("name"))
        env_pairs = pair_env(lines)
        outside = [l for l in lines if not ENV_LINE.match(l)]
        check(not [l for l in outside if "PRIVATE KEY-----" in l],
              "%s: PEM key armour in a log line (suspected exposure; not reproduced)" % j.get("name"))
        if mode == "preview":
            # Neither value may reach ANY executed step of the run.
            for nm, val in env_pairs:
                check(val == "", "%s: %s reached a step (%s)" % (j.get("name"), nm, "masked" if val == "***" else "unmasked"))
        if j in leg_jobs:
            log = "\n".join(lines)
            if mode == "full":
                fwg = framework_group(lines)
                fw_env = dict(pair_env(fwg)) if fwg else {}
                ok = check(fwg is not None, "%s: framework step block not found" % j["name"])
                for nm in NAMES:
                    ok &= check(fw_env.get(nm) == "***", "%s: %s did not reach the framework step (%r)" % (j["name"], nm, fw_env.get(nm)))
                ok &= check("[repo-auth] client certificate: complete" in log, "%s: pair not complete at the check" % j["name"])
                ok &= check("Installed client certificate, pgEdge repositories enabled" in log, "%s: certificate not installed" % j["name"])
            else:
                ok = check("[repo-auth] client certificate:" not in log, "%s: repo-auth check ran in a preview" % j["name"])
            rows.append((j["name"], "ok" if ok else "PROBLEM"))
    check(heads and all(STABLE_SHA.startswith(h) for h in heads), "PEP checkouts at %r, expected %s" % (sorted(heads), STABLE_SHA[:7]))
    return problems, rows, out


def main(argv):
    mode, evidence = argv[1], argv[2]
    env = os.environ
    source = Fixtures(env["VERIFY_FIXTURES"]) if env.get("VERIFY_FIXTURES") else Live(env)
    problems, rows, out = verify(mode, evidence, source, env)
    summary = env.get("GITHUB_STEP_SUMMARY")
    if summary:
        with open(summary, "a") as f:
            f.write("## Chain check (%s) through PEP @%s = %s\n\n" % (mode, STABLE_REF, STABLE_SHA[:7]))
            f.write("Validation harness: nothing built or published; the packages and receipts are validation "
                    "fixtures; %s\n\n" % ("the `success` publication inputs are synthetic." if mode == "full"
                                          else "simulated run, publication `skipped`."))
            f.write("certification_state=`%s` execution_status=`%s` test_verdict=`%s` coverage_status=`%s`\n\n"
                    % tuple(out.get(k) for k in ("certification_state", "execution_status", "test_verdict", "coverage_status")))
            f.write("| PEP test leg | credential evidence |\n|---|---|\n")
            for r in rows:
                f.write("| %s | %s |\n" % r)
            f.write("\n**%s**\n" % ("PASS" if not problems else "FAIL: %d problem(s)" % len(problems)))
            for p in problems:
                f.write("- %s\n" % p)
    print("\n".join(problems) if problems else "chain evidence as expected")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
