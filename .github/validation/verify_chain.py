#!/usr/bin/env python3
"""Evidence check for the temporary PEP chain check, full, all 14 cells. Validation harness only.

Run by the chain workflow's assert job; also run offline against fixtures. Usage:

    verify_chain.py full <evidence-dir>

The evidence directory holds this run's downloaded PEP artifacts (pep-certification-*,
pep-invocation-plan-*, pep-capture-evidence-*, pep-cert-plan-*, pep-summary-*), all named
`-r<run number>-a<attempt>` of this attempt. Run metadata, the attempt's jobs (every page) and
their logs come from the GitHub API, or, when VERIFY_FIXTURES names a directory, from run.json,
jobs.json and logs/<job id>.log there. NEEDS_JSON is the assert job's toJSON(needs). SELF_JOB_ID
is the assert job's own id (job.check_run_id): that job is still running, so it is the one job
whose log is not read; every other job must have completed with a readable log.
Exit status 0 only if the evidence matches the expectations below.

The adapter is called through the branch reference @v1_STABLE. The reference and the commit it
resolved to are checked separately: GitHub records the adapter as `...@v1_STABLE` (ref
refs/heads/v1_STABLE) and each nested PEP workflow as `...@<commit>`; all must be STABLE_SHA, as
must PEP's own provenance records and its checkouts.

Coverage is counted from evidence, never from job colour: each of the 14 detector cells must be
accepted once by capture, and each of its 42 legs (cell x PG16/17/18) must appear exactly once
in the plan, in the result, as a leg job with certificate evidence in its log, and as a summary
artifact of THIS run with its install evidence and JUnit report. A missing piece is missing
coverage, not a pass. The step summary shows a per-cell coverage table.

What the run is: the packages are the already-published pgedge-rag-server2 2.0.0 release
packages, downloaded and checked against rag-fixtures.json (sha256 from the repository
metadata), and the receipts are recorded in the run. Nothing is built or published, and no
original build provenance is established. The `success` publication inputs are synthetic.
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
import xml.etree.ElementTree as ET
from collections import Counter
from pathlib import Path
from urllib.parse import urlsplit

STABLE_REF = "v1_STABLE"
STABLE_SHA = "9b3c9e964487568a7b3f3a8f0a3c68000fe0d943"   # v1_STABLE after PEP #38 merged
BRANCH = "validate/pep-v1-stable-rag-full14"
WORKFLOW = ".github/workflows/pep-chain-check-full.yml"
PEP_WF = "pgEdge/pgedge-pep-test/.github/workflows/"
NESTED = ("pep-capture.yml", "pep-certify.yml", "pep-integration.yml")
HEAD_LINE = re.compile(r"HEAD is now at ([0-9a-f]{7,40}) ")
NAMES = ("PGEDGE_REPO_CLIENT_CRT", "PGEDGE_REPO_CLIENT_KEY")
FIXTURES = Path(__file__).resolve().parent / "rag-fixtures.json"
PACKAGE, VERSION, BUILDNUM, TAG, CHANNEL = "pgedge-rag-server2", "2.0.0", "1", "v2.0.0", "release"
PG = ("16", "17", "18")
# The 14 detector cells of the published release: detector image, arch, the container PEP plans.
CELLS = {
    "pepcell.v1.rpm.el-9.amd64.pkg":      ("almalinux:9", "amd64", "oel9-amd64"),
    "pepcell.v1.rpm.el-9.arm64.pkg":      ("almalinux:9", "arm64", "rocky9-arm64"),
    "pepcell.v1.rpm.el-10.amd64.pkg":     ("almalinux:10", "amd64", "alma10-amd64"),
    "pepcell.v1.rpm.el-10.arm64.pkg":     ("almalinux:10", "arm64", "alma10-arm64"),
    "pepcell.v1.deb.jammy.amd64.pkg":     ("ubuntu:jammy", "amd64", "ubuntu2204-amd64"),
    "pepcell.v1.deb.jammy.arm64.pkg":     ("ubuntu:jammy", "arm64", "ubuntu2204-arm64"),
    "pepcell.v1.deb.noble.amd64.pkg":     ("ubuntu:noble", "amd64", "ubuntu2404-amd64"),
    "pepcell.v1.deb.noble.arm64.pkg":     ("ubuntu:noble", "arm64", "ubuntu2404-arm64"),
    "pepcell.v1.deb.resolute.amd64.pkg":  ("ubuntu:resolute", "amd64", "ubuntu2604-amd64"),
    "pepcell.v1.deb.resolute.arm64.pkg":  ("ubuntu:resolute", "arm64", "ubuntu2604-arm64"),
    "pepcell.v1.deb.bookworm.amd64.pkg":  ("debian:bookworm", "amd64", "debian12-amd64"),
    "pepcell.v1.deb.bookworm.arm64.pkg":  ("debian:bookworm", "arm64", "debian12-arm64"),
    "pepcell.v1.deb.trixie.amd64.pkg":    ("debian:trixie", "amd64", "debian13-amd64"),
    "pepcell.v1.deb.trixie.arm64.pkg":    ("debian:trixie", "arm64", "debian13-arm64"),
}
LEGS = {(cid, pg) for cid in CELLS for pg in PG}
IDENTITY = {"l1": "proven", "l2a": "proven", "l2b": "not_attempted"}
JUNIT = {"tests": 12, "failures": 0, "errors": 0, "skipped": 3}
SKIPS = {"test_rag_binary_version": "integration mode: superseded by test_rag_identity",
         "test_rag_component_version": "integration mode: superseded by test_rag_identity",
         "test_rag_component_upgrade": "Skipping upgrade tests because UPGRADE=false in env"}
# Every leg runs exactly these test functions once each (run 41037649715's RAG JUnit); only the
# parametrized id after "[" differs by platform.
TESTS = ("test_install_prerequisites", "test_configure_repository", "test_rag_component_install",
         "test_rag_component_upgrade", "test_rag_component_version", "test_verify_bundled_files", "test_verify_sbom",
         "test_rag_binary_version", "test_rag_identity", "test_rag_binary_stripped", "test_rag_component_uninstall",
         "test_pgedge_cleanup")
OUTPUTS = {"certification_state": "pass", "execution_status": "completed", "test_verdict": "pass",
           "coverage_status": "complete"}
DECISION = {"certification_state": "pass", "policy_decision": "allow", "workflow_conclusion": "success",
            "reason_code": "clean_pass", "requested_mode": "observe"}
CAPTURE_COUNTS = {"planned_cells": 14, "receipt_candidates": 14, "accepted_receipt_cells": 14,
                  "rejected_receipt_cells": 0, "ambiguous_receipt_cells": 0, "absent_receipt_cells": 0,
                  "verified_package_artifacts": 14, "verified_members": 14}
CERT_PLAN_DENOMS = {"planned_build_cells": 14, "available_build_cells": 14, "selected_targets": 14, "eligible_targets": 14}
PLAN_COUNTS = {"invocations": 42, "planned_cells": 14, "selected_targets": 14, "eligible_targets": 14,
               "covered_targets": 14, "coverage_gaps": 0, "gaps_by_scope": {"cell": 0, "member": 0, "target": 0}}
COUNTS = {"expected_invocations": 42, "matched": 42, "completed": 42, "pass": 42, "fail": 0, "incomplete": 0,
          "infra_failure": 0, "not_run": 0, "preview": 0, "missing": 0, "unexpected_results": 0, "planner_gaps": 0}
RELEASE = {"logical_component": "rag", "intended_version": VERSION, "intended_buildnum": BUILDNUM,
           "effective_tag": TAG, "channel": CHANNEL}
CERTIFY = "Certify packages (PEP)"
LEG_JOB = CERTIFY + " / certify / %s · PG%s · " + PACKAGE + " / full test (install and product tests)"
ASSERT_JOB = "assert chain evidence (full)"
OTHER_JOBS = ("Detect build matrix", "Select all 14 detector cells",
              "Fixed validation identity (synthetic inputs; nothing is published)",
              CERTIFY + " / Release inputs", CERTIFY + " / certify / identity",
              CERTIFY + " / certify / capture / capture", CERTIFY + " / certify / plan",
              CERTIFY + " / certify / aggregate", CERTIFY + " / certify / enforce",
              CERTIFY + " / Release certification summary")
# Jobs that check out PEP (as in run 41037649715): each exactly once, at STABLE_SHA.
CHECKOUT_JOBS = (CERTIFY + " / Release inputs", CERTIFY + " / certify / capture / capture",
                 CERTIFY + " / certify / plan", CERTIFY + " / certify / aggregate",
                 CERTIFY + " / Release certification summary")

CONTROL = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-_]|[\x00-\x08\x0b-\x1f\x7f-\x9f]")
ENV_LINE = re.compile(r"^\S+\s+(PGEDGE_REPO_CLIENT_(?:CRT|KEY)):(?: (.*))?$")
PEM = re.compile(rb"-----(?:BEGIN|END) [A-Z0-9 ]+-----")


def dist(cid):
    """The release suffix of a cell's package: el-9 -> el9, bookworm -> bookworm."""
    fam, os_token = cid.split(".")[2:4]
    return os_token.replace("-", "") if fam == "rpm" else os_token


def package_job(cid):
    image, arch, _ = CELLS[cid]
    return "Package %s (%s %s) [pep-cell:%s]" % (cid.split(".")[2].upper(), image, arch, cid)


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

    def jobs_page(self, page):
        return self._gh("repos/%s/actions/runs/%s/attempts/%s/jobs?per_page=100&page=%d" % (self.repo, self.run_id, self.att, page))

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
    """run.json, jobs.json ({"total_count", "jobs"}, served 100 per page as the API does), logs/<id>.log."""

    def __init__(self, d):
        self.d = Path(d)

    def run(self):
        return json.loads((self.d / "run.json").read_text())

    def jobs_page(self, page):
        doc = json.loads((self.d / "jobs.json").read_text())
        jobs = doc["jobs"]
        return {"total_count": doc.get("total_count", len(jobs)), "jobs": jobs[(page - 1) * 100:page * 100]}

    def log(self, jid):
        return (self.d / "logs" / ("%s.log" % jid)).read_text()


def all_jobs(source):
    """Every job of this attempt, page by page until total_count jobs are read (a run can exceed 100)."""
    jobs, page, total = [], 1, None
    while True:
        doc = source.jobs_page(page)
        batch = doc.get("jobs") or []
        if total is None:
            total = doc.get("total_count")
            if not isinstance(total, int) or total < 0:
                raise RuntimeError("jobs listing has no usable total_count: %r" % (total,))
        elif doc.get("total_count") != total:
            raise RuntimeError("jobs listing total_count changed between pages (%r, %r)" % (total, doc.get("total_count")))
        jobs.extend(batch)
        if len(jobs) >= total or not batch or page >= 20:
            break
        page += 1
    ids = [j.get("id") for j in jobs]
    if len(jobs) != total or len(set(ids)) != len(ids):
        raise RuntimeError("jobs listing incomplete: %d job(s), %d distinct, over %d page(s), total_count %d"
                           % (len(jobs), len(set(ids)), page, total))
    return jobs


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


def _load(p):
    return json.loads(Path(p).read_text())


def junit_outcome(d):
    """(counts, sorted (name, outcome, message)) from every JUnit XML under a summary; all must agree."""
    seen = set()
    for x in sorted(d.rglob("*.xml")):
        suites = ET.parse(str(x)).getroot()
        suites = [suites] if suites.tag == "testsuite" else suites.findall("testsuite")
        counts = {k: sum(int(s.get(k, "0")) for s in suites) for k in JUNIT}
        cases = []
        for s in suites:
            for tc in s.findall("testcase"):
                out, msg = "passed", ""
                for kind in ("failure", "error", "skipped"):
                    el = tc.find(kind)
                    if el is not None:
                        out, msg = kind, el.get("message", "")
                        break
                cases.append((tc.get("name", ""), out, msg))
        seen.add((tuple(sorted(counts.items())), tuple(sorted(cases))))
    if len(seen) != 1:
        raise ValueError("%d distinct JUnit reports" % len(seen))
    counts, cases = seen.pop()
    return dict(counts), list(cases)


def verify(mode, evidence, source, env):
    run_id, att, run_no = env["GITHUB_RUN_ID"], env["GITHUB_RUN_ATTEMPT"], env["GITHUB_RUN_NUMBER"]
    this_run = (run_id, att)
    suffix = "-r%s-a%s" % (run_no, att)
    ev = Path(evidence)
    problems = []
    # Per leg: which evidence was found and sound. A leg passes only with all five.
    leg = {k: {"plan": None, "result": None, "summary": None, "job": None, "verdict": None} for k in LEGS}
    capture_verdict = {}
    gaps = {}

    def check(ok, what):
        if not ok:
            problems.append(what)
        return ok

    def mark(key, part, ok):
        if key in leg:
            leg[key][part] = bool(ok) if leg[key][part] is None else False   # a second sighting is a duplicate

    out = (json.loads(env["NEEDS_JSON"]).get("certify") or {})
    check(mode == "full", "mode %r: this harness is full certification only" % mode)

    # 0. The fixture table: the independent sha256 of every published package.
    sha, pin = {}, {}
    try:
        fx = _load(FIXTURES)["cells"]
        check(sorted(fx) == sorted(CELLS), "fixture cells %r, expected the 14 detector cells" % sorted(fx))
        for cid in CELLS:
            files = fx.get(cid) or []
            if check(len(files) == 1 and files[0].get("name") == PACKAGE and files[0].get("version") == "%s-%s.%s" % (VERSION, BUILDNUM, dist(cid))
                     and re.fullmatch(r"[0-9a-f]{64}", files[0].get("sha256") or ""), "fixture %s: %r" % (cid, files)):
                sha[cid] = files[0]["sha256"]
                pin[cid] = "%s-%s.%s" % (VERSION, BUILDNUM, dist(cid))
    except (OSError, ValueError, KeyError, TypeError) as e:
        problems.append("fixture table: %s" % e)

    # 1. The adapter's own outputs: every axis, not just the state.
    check(out.get("result") == "success", "certify job result %r" % out.get("result"))
    for k, v in OUTPUTS.items():
        check((out.get("outputs") or {}).get(k) == v, "certify output %s=%r, expected %r" % (k, (out.get("outputs") or {}).get(k), v))

    # 2. Only this attempt's evidence, each PEP document exactly once.
    for d in sorted(p for p in ev.iterdir() if p.is_dir()) if ev.is_dir() else []:
        check(d.name.endswith(suffix), "%s: evidence not of this attempt (%s)" % (d.name[:80], suffix))

    def one(prefix, filename):
        hits = sorted(ev.glob("%s%s/%s" % (prefix, suffix, filename)))
        if len(hits) != 1:
            raise ValueError("%d %s%s/%s found" % (len(hits), prefix, suffix, filename))
        return _load(hits[0])

    # 3. Capture accepted exactly the 14 receipts, once each.
    try:
        cap = one("pep-capture-evidence", "capture-evidence.json")
        c = cap.get("counts") or {}
        check(c == CAPTURE_COUNTS, "capture counts %r" % c)
        cells = cap.get("cells") or []
        ids = Counter(x.get("cell_id") for x in cells)
        check(set(ids) == set(CELLS) and all(n == 1 for n in ids.values()), "capture cells %r, expected the 14 once each" % sorted(ids))
        for x in cells:
            cid = x.get("cell_id")
            ok = (x.get("verdict"), x.get("code")) == ("accepted", None) and all(
                isinstance(x.get(k), int) and x.get(k) > 0 for k in ("receipt_artifact_id", "package_artifact_id"))
            check(ok, "capture %s: %r" % (cid, x))
            capture_verdict[cid] = x.get("verdict") if ids.get(cid) == 1 else "duplicate"
        cp = cap.get("provenance") or {}
        check((cp.get("pep_implementation_ref"), cp.get("pep_resolved_sha")) == (STABLE_SHA, STABLE_SHA),
              "capture provenance %r" % ((cp.get("pep_implementation_ref"), cp.get("pep_resolved_sha")),))
        check((cp.get("run_id"), cp.get("run_attempt")) == this_run, "capture from run %r" % ((cp.get("run_id"), cp.get("run_attempt")),))
    except (OSError, ValueError) as e:
        problems.append("capture evidence: %s" % e)
    try:
        cpl = one("pep-cert-plan", "cert-plan.json")
        check(cpl.get("plan_resolved") is True and cpl.get("execution_mode") == mode and not cpl.get("errors"),
              "cert-plan resolved/mode/errors %r" % ((cpl.get("plan_resolved"), cpl.get("execution_mode"), cpl.get("errors")),))
        cd = cpl.get("coverage_denominators") or {}
        check({k: cd.get(k) for k in CERT_PLAN_DENOMS} == CERT_PLAN_DENOMS, "cert-plan denominators %r" % cd)
        ids = Counter(x.get("cell_id") for x in cpl.get("cells") or [])
        check(set(ids) == set(CELLS) and all(n == 1 for n in ids.values()), "cert-plan cells %r, expected the 14 once each" % sorted(ids))
        for x in cpl.get("cells") or []:
            shas = [m.get("sha256") for m in x.get("members") or []]
            check(x.get("build_state") == "available" and shas == [sha.get(x.get("cell_id"))],
                  "cert-plan %s: %r, member sha256 %r" % (x.get("cell_id"), x.get("build_state"), shas))
        cpp = cpl.get("provenance") or {}
        check((cpp.get("run_id"), cpp.get("run_attempt")) == this_run, "cert-plan from run %r" % ((cpp.get("run_id"), cpp.get("run_attempt")),))
    except (OSError, ValueError) as e:
        problems.append("cert-plan: %s" % e)

    # 4. The plan: 42 invocations, one per cell and PG major, on the right container; no gaps.
    by_iid = {}
    try:
        plan = one("pep-invocation-plan", "invocation-plan.json")
        pc = plan.get("counts") or {}
        check(plan.get("plan_resolved") is True and plan.get("execution_mode") == mode and not plan.get("errors"),
              "plan resolved/mode/errors %r" % ((plan.get("plan_resolved"), plan.get("execution_mode"), plan.get("errors")),))
        check(pc == PLAN_COUNTS, "plan counts %r" % pc)
        check(plan.get("supported_pg_majors") == list(PG), "plan PG majors %r" % plan.get("supported_pg_majors"))
        check(plan.get("release") == RELEASE, "plan release %r" % plan.get("release"))
        pp = plan.get("provenance") or {}
        check((pp.get("run_id"), pp.get("run_attempt")) == this_run, "plan from run %r" % ((pp.get("run_id"), pp.get("run_attempt")),))
        check(not plan.get("uncertified_runtime_packages"), "plan uncertified %r" % plan.get("uncertified_runtime_packages"))
        for g in plan.get("coverage_gaps") or []:
            gaps.setdefault(g.get("cell_id"), []).append(g.get("reason"))
        check(not plan.get("coverage_gaps"), "plan coverage_gaps %r" % plan.get("coverage_gaps"))
        for inv in (plan.get("matrix") or {}).get("include") or []:
            cid, pg = inv.get("source_cell_id"), inv.get("pg_major")
            tag = "plan %s pg%s: " % (cid, pg)
            if not check((cid, pg) in LEGS, tag + "not an expected leg"):
                continue
            fam, arch = cid.split(".")[2], CELLS[cid][1]
            native = {"amd64": "x86_64", "arm64": "aarch64"}[arch] if fam == "rpm" else arch
            want = {"component": "rag", "package_name": PACKAGE, "channel": CHANNEL, "expected_version": VERSION,
                    "expected_buildnum": BUILDNUM, "effective_tag": TAG, "family": fam, "arch": arch,
                    "container_alias": CELLS[cid][2], "expected_binary": "",
                    "expected_rpm": pin.get(cid) if fam == "rpm" else "", "expected_deb": pin.get(cid) if fam == "deb" else "",
                    "source_target_id": cid + "::" + PACKAGE, "producer_repo": env.get("GITHUB_REPOSITORY"),
                    "package": {"name": PACKAGE, "version": VERSION, "release": "%s.%s" % (BUILDNUM, dist(cid)),
                                "native_arch": native, "sha256": sha.get(cid)}}
            bad = sorted(k for k, v in want.items() if inv.get(k) != v)
            mark((cid, pg), "plan", check(not bad, tag + "%s differ: %r" % (", ".join(bad), {k: inv.get(k) for k in bad})))
            by_iid.setdefault(inv.get("invocation_id"), []).append(((cid, pg), inv))
        check(all(len(v) == 1 for v in by_iid.values()), "plan invocation ids not unique")
    except (OSError, ValueError) as e:
        problems.append("invocation plan: %s" % e)
    iid_leg = {iid: v[0][0] for iid, v in by_iid.items() if len(v) == 1}
    planned = {iid: v[0][1] for iid, v in by_iid.items() if len(v) == 1}

    # 5. The certification result, its decision, and every leg in it.
    try:
        res = one("pep-certification", "cert-result.json")
        dec = one("pep-certification", "cert-decision.json")
        check({k: dec.get(k) for k in DECISION} == DECISION, "decision %r" % {k: dec.get(k) for k in DECISION})
        check(dec.get("axes") == {"result_resolved": True, "execution_status": "completed", "test_verdict": "pass",
                                  "coverage_status": "complete", "reason_code": None}, "decision axes %r" % dec.get("axes"))
        for k in ("execution_status", "test_verdict", "coverage_status"):
            check(res.get(k) == OUTPUTS[k], "result %s=%r" % (k, res.get(k)))
        check(res.get("result_resolved") is True and res.get("reason_code") is None and not res.get("errors"),
              "result resolved/reason/errors %r" % ((res.get("result_resolved"), res.get("reason_code"), res.get("errors")),))
        rp = res.get("provenance") or {}
        check((rp.get("pep_requested_ref"), rp.get("pep_resolved_sha")) == (STABLE_SHA, STABLE_SHA),
              "result provenance %r" % ((rp.get("pep_requested_ref"), rp.get("pep_resolved_sha")),))
        check((rp.get("run_id"), rp.get("run_attempt")) == this_run, "result from run %r" % ((rp.get("run_id"), rp.get("run_attempt")),))
        check(res.get("release") == RELEASE, "result release %r" % res.get("release"))
        for k in ("coverage_gaps", "unexpected_results", "uncertified_runtime_packages"):
            check(not res.get(k), "result %s %r" % (k, res.get(k)))
        rc = res.get("counts") or {}
        for k, v in COUNTS.items():
            check(rc.get(k) == v, "result count %s=%r, expected %r" % (k, rc.get(k), v))
        legs = res.get("legs") or []
        keys = Counter(((l.get("planned_invocation") or {}).get("source_cell_id"), (l.get("planned_invocation") or {}).get("pg_major")) for l in legs)
        check(len(legs) == 42 and set(keys) == LEGS and all(n == 1 for n in keys.values()),
              "result legs: %d, missing %r, extra %r, repeated %r" % (len(legs), sorted(LEGS - set(keys)),
                                                                     sorted(set(keys) - LEGS), sorted(k for k, n in keys.items() if n > 1)))
        for l in legs:
            pi = l.get("planned_invocation") or {}
            key = (pi.get("source_cell_id"), pi.get("pg_major"))
            tag = "leg %s pg%s: " % key
            if not check(key in LEGS, tag + "not an expected leg"):
                continue
            cid = key[0]
            ok = check(pi == planned.get(l.get("invocation_id")) and iid_leg.get(l.get("invocation_id")) == key,
                       tag + "planned invocation differs from the plan")
            ok &= check(pi.get("container_alias") == CELLS[cid][2], tag + "container %r" % pi.get("container_alias"))
            ok &= check((pi.get("package") or {}).get("sha256") == sha.get(cid), tag + "receipt sha256 %r" % (pi.get("package") or {}).get("sha256"))
            ok &= check((l.get("reconciliation"), l.get("execution_status"), l.get("test_verdict")) == ("matched", "completed", "pass"),
                        tag + "status/verdict %r" % ((l.get("reconciliation"), l.get("execution_status"), l.get("test_verdict")),))
            ok &= check(l.get("package_digest") == "match", tag + "package_digest %r" % l.get("package_digest"))
            ok &= check(l.get("installed_package_sha256") == sha.get(cid), tag + "installed sha256 %r" % l.get("installed_package_sha256"))
            ok &= check(l.get("identity_evidence") == IDENTITY and not l.get("unproven_identity_rungs"), tag + "identity %r" % l.get("identity_evidence"))
            ok &= check(l.get("counts") == JUNIT, tag + "counts %r" % l.get("counts"))
            lp = l.get("provenance") or {}
            ok &= check((lp.get("pep_requested_ref"), lp.get("pep_resolved_sha")) == (STABLE_SHA, STABLE_SHA),
                        tag + "provenance %r" % lp.get("pep_resolved_sha"))
            ok &= check((lp.get("caller_run_id"), lp.get("caller_run_attempt")) == this_run,
                        tag + "from run %r" % ((lp.get("caller_run_id"), lp.get("caller_run_attempt")),))
            mark(key, "result", ok)
            leg[key]["verdict"] = l.get("test_verdict")
    except (OSError, ValueError) as e:
        problems.append("certification result: %s" % e)

    # 6. Each leg's own uploaded evidence: exactly one summary of THIS run per planned leg.
    summaries = sorted(ev.glob("pep-summary-*")) if ev.is_dir() else []
    check(len(summaries) == 42, "%d leg summaries, expected 42" % len(summaries))
    for d in summaries:
        tag = d.name[:72] + ": "
        try:
            s = _load(d / "summary.json")
        except (OSError, ValueError) as e:
            problems.append(tag + "summary: %s" % e)
            continue
        iid = s.get("invocation_id")
        key = iid_leg.get(iid)
        if not check(key is not None and d.name.endswith("-%s%s" % (iid, suffix)), tag + "summary not of this attempt or for an unplanned invocation (%r)" % iid):
            continue
        cid, pg = key
        fam, (_, arch, alias) = cid.split(".")[2], CELLS[cid]
        tag = "summary %s pg%s: " % key
        sp = s.get("provenance") or {}
        ok = check((sp.get("pep_requested_ref"), sp.get("pep_resolved_sha")) == (STABLE_SHA, STABLE_SHA),
                   tag + "provenance %r" % ((sp.get("pep_requested_ref"), sp.get("pep_resolved_sha")),))
        ok &= check((sp.get("caller_run_id"), sp.get("caller_run_attempt")) == this_run,
                    tag + "from run %r, not this run %r" % ((sp.get("caller_run_id"), sp.get("caller_run_attempt")), this_run))
        ok &= check((sp.get("caller_ref"), sp.get("caller_sha")) == ("refs/heads/" + BRANCH, env.get("GITHUB_SHA")),
                    tag + "caller %r" % ((sp.get("caller_ref"), sp.get("caller_sha")),))
        ok &= check((s.get("execution_status"), s.get("test_verdict")) == ("completed", "pass"), tag + "%r" % ((s.get("execution_status"), s.get("test_verdict")),))
        ok &= check(s.get("counts") == JUNIT, tag + "counts %r" % s.get("counts"))
        ok &= check(s.get("identity_evidence") == IDENTITY, tag + "identity %r" % s.get("identity_evidence"))
        ok &= check(s.get("installed_package_sha256") == sha.get(cid), tag + "installed sha256 %r" % s.get("installed_package_sha256"))
        target = {"package_name": PACKAGE, "family": fam, "arch": arch, "pg_major": pg, "container_alias": alias, "channel": CHANNEL}
        try:
            ie = _load(d / "install-evidence.json")
            ok &= check((ie.get("installed"), ie.get("install_kind"), ie.get("install_token"), ie.get("expected_version"))
                        == (True, "pinned", pin.get(cid), VERSION),
                        tag + "install %r" % ((ie.get("installed"), ie.get("install_kind"), ie.get("install_token"), ie.get("expected_version")),))
            ok &= check(ie.get("installed_sha256") == sha.get(cid), tag + "install evidence sha256 %r" % ie.get("installed_sha256"))
            ok &= check(ie.get("target") == target, tag + "install target %r" % ie.get("target"))
            ok &= check(ie.get("run_token") == "%s-%s" % this_run, tag + "install run_token %r" % ie.get("run_token"))
            oi = _load(d / "observed-identity.json")
            ok &= check(oi.get("target") == target and (oi.get("observed") or {}).get("package_manager_version") == pin.get(cid),
                        tag + "observed identity %r" % oi.get("observed"))
        except (OSError, ValueError) as e:
            ok = check(False, tag + "install/identity evidence: %s" % e)
        try:
            counts, cases = junit_outcome(d)
            skipped = sorted((n.split("[")[0], m) for n, o, m in cases if o == "skipped")
            ok &= check(counts == JUNIT and len(cases) == JUNIT["tests"], tag + "JUnit counts %r over %d case(s)" % (counts, len(cases)))
            ok &= check(not [n for n, o, _ in cases if o in ("failure", "error")], tag + "JUnit failures %r" % [n for n, o, _ in cases if o in ("failure", "error")])
            ok &= check(skipped == sorted(SKIPS.items()), tag + "JUnit skips %r" % skipped)
            names = Counter(n.split("[")[0] for n, _, _ in cases)
            ok &= check(names == Counter(TESTS), tag + "JUnit test names differ: missing %s, unexpected %s"
                        % (sorted((Counter(TESTS) - names).elements()), sorted((names - Counter(TESTS)).elements())))
        except (OSError, ValueError, ET.ParseError) as e:
            ok = check(False, tag + "JUnit report: %s" % e)
        mark(key, "summary", ok)

    # 7. No PEM armour anywhere in the downloaded evidence (location only, never content).
    for f in (p for p in ev.rglob("*") if p.is_file()) if ev.is_dir() else []:
        check(not PEM.search(f.read_bytes()), "%s: PEM armour in an artifact file (suspected exposure; not reproduced)" % f.relative_to(ev))

    # 8. Which PEP code ran, and which run this is. The adapter's reference and the commit it
    #    resolved to are separate checks; the nested workflows GitHub records under the commit.
    try:
        run = source.run()
        check((str(run.get("id")), str(run.get("run_attempt"))) == this_run, "run metadata is run %r" % ((run.get("id"), run.get("run_attempt")),))
        check((run.get("head_branch"), run.get("event"), run.get("path"), run.get("head_sha")) == (BRANCH, "push", WORKFLOW, env.get("GITHUB_SHA")),
              "run is %r, expected a push of %s running %s" % ((run.get("head_branch"), run.get("event"), run.get("path"), run.get("head_sha")), BRANCH, WORKFLOW))
        refs = [(r.get("path"), r.get("ref"), r.get("sha")) for r in run.get("referenced_workflows") or []]
        adapter = [r for r in refs if (r[0] or "").split("@")[0] == PEP_WF + "pep-release-certify.yml"]
        nested = sorted((r for r in refs if r not in adapter), key=repr)
        check([r[:2] for r in adapter] == [(PEP_WF + "pep-release-certify.yml@" + STABLE_REF, "refs/heads/" + STABLE_REF)],
              "adapter not called through @%s: %r" % (STABLE_REF, adapter))
        check([r[2] for r in adapter] == [STABLE_SHA],
              "adapter resolved to %r, expected %s" % ([r[2] for r in adapter], STABLE_SHA))
        check(nested == sorted(((PEP_WF + n + "@" + STABLE_SHA, "refs/heads/" + STABLE_REF, STABLE_SHA) for n in NESTED), key=repr),
              "nested PEP workflows %r, expected %s at %s" % (nested, ", ".join(NESTED), STABLE_SHA))
    except (RuntimeError, OSError, ValueError) as e:
        problems.append("run metadata: %s" % e)
    try:
        jobs = all_jobs(source)
    except (RuntimeError, OSError, ValueError, KeyError) as e:
        problems.append("jobs: %s" % e)
        jobs = []

    # 9. The jobs: exactly the expected set, once each.
    leg_name = {LEG_JOB % (CELLS[cid][2], pg): (cid, pg) for cid, pg in LEGS}
    want = Counter(list(OTHER_JOBS) + [package_job(cid) for cid in CELLS] + list(leg_name) + [ASSERT_JOB])
    got = Counter(j.get("name") for j in jobs)
    if jobs:
        check(not (want - got), "jobs missing: %r" % sorted(want - got))
        check(not (got - want), "unexpected or repeated jobs: %r" % sorted(got - want))

    # This assert job has no log until it finishes, so it alone is skipped, and only when
    # SELF_JOB_ID names it exactly. Every other job is upstream of it: completed, log required.
    heads = set()
    own_id = env.get("SELF_JOB_ID", "")
    own = [j for j in jobs if str(j.get("id")) == own_id]
    if not check(own_id.isdigit() and len(own) == 1 and own[0].get("name") == ASSERT_JOB,
                 "own assertion job: SELF_JOB_ID %r does not name %r in this attempt" % (own_id, ASSERT_JOB)):
        own = []
    for j in jobs:
        if j in own:
            continue
        name = j.get("name")
        key = leg_name.get(name)
        ok = check(j.get("status") == "completed", "%s: required job not completed (%r)" % (name, j.get("status")))
        ok = ok and check(j.get("conclusion") == "success", "%s: concluded %r" % (name, j.get("conclusion")))
        try:
            lines = CONTROL.sub("", source.log(j["id"])).splitlines()
        except (RuntimeError, OSError) as e:
            problems.append("%s: %s" % (name, e))
            mark(key, "job", False)
            continue
        found = HEAD_LINE.findall("\n".join(lines))
        heads |= set(found)
        if key or name in CHECKOUT_JOBS:
            ok &= check(len(found) == 1, "%s: %d PEP checkout(s), expected 1" % (name, len(found)))
        pem = [i + 1 for i, l in enumerate(lines) if PEM.search(l.encode())]
        ok &= check(not pem, "%s: PEM armour at log line(s) %s (suspected exposure; not reproduced)" % (name, pem[:5]))
        # The pair reaches the test legs only, and only masked; elsewhere it is empty.
        for nm, val in pair_env(lines):
            ok &= check(val in ("", "***"), "%s: %s listed unmasked (value not reproduced)" % (name, nm))
            ok &= check(key is not None or val == "", "%s: %s reached a non-leg step (masked)" % (name, nm))
        if key:
            log = "\n".join(lines)
            fwg = framework_group(lines)
            fw_env = dict(pair_env(fwg)) if fwg else {}
            ok &= check(fwg is not None, "%s: framework step block not found" % name)
            for nm in NAMES:
                ok &= check(fw_env.get(nm) == "***", "%s: %s did not reach the framework step (%r)" % (name, nm, fw_env.get(nm)))
            ok &= check("[repo-auth] client certificate: complete" in log, "%s: pair not complete at the check" % name)
            ok &= check("Installed client certificate, pgEdge repositories enabled" in log, "%s: certificate not installed" % name)
            mark(key, "job", ok)
    check(heads and all(STABLE_SHA.startswith(h) for h in heads), "PEP checkouts at %r, expected %s" % (sorted(heads), STABLE_SHA[:7]))

    # 10. Coverage from evidence: a leg is tested only with plan, result, summary and job all sound.
    rows = []
    for cid in sorted(CELLS, key=lambda c: (c.split(".")[2], CELLS[c][2])):
        cols = []
        for pg in PG:
            e = leg[(cid, pg)]
            absent = [p for p in ("plan", "result", "summary", "job") if e[p] is None]
            if cid in gaps:
                cols.append("gap: " + ", ".join(map(str, gaps[cid])))
            elif absent:
                cols.append("missing: " + ", ".join(absent))
            elif e["verdict"] == "fail":
                cols.append("tested-fail")
            elif all(e[p] for p in ("plan", "result", "summary", "job")) and e["verdict"] == "pass":
                cols.append("tested-pass")
            else:
                cols.append("PROBLEM: " + ", ".join(p for p in ("plan", "result", "summary", "job") if not e[p]))
        rows.append((cid, CELLS[cid][2], capture_verdict.get(cid, "missing")) + tuple(cols))
    short = ["%s PG%s %s" % (r[0], pg, c) for r in rows for pg, c in zip(PG, r[3:]) if c != "tested-pass"]
    check(not short, "%d of 42 legs tested-pass from evidence; not: %s" % (42 - len(short), "; ".join(short[:6]) + ("; ..." if len(short) > 6 else "")))
    return problems, rows, out.get("outputs") or {}


def main(argv):
    if len(argv) != 3:
        sys.stderr.write("usage: verify_chain.py full <evidence-dir>\n")
        return 2
    mode, evidence = argv[1], argv[2]
    env = os.environ
    source = Fixtures(env["VERIFY_FIXTURES"]) if env.get("VERIFY_FIXTURES") else Live(env)
    problems, rows, out = verify(mode, evidence, source, env)
    summary = env.get("GITHUB_STEP_SUMMARY")
    if summary:
        with open(summary, "a") as f:
            f.write("## Chain check (full, all 14 cells) through PEP @%s = %s\n\n" % (STABLE_REF, STABLE_SHA[:7]))
            f.write("Validation harness: nothing built or published; the packages are the already-published "
                    "pgedge-rag-server2 2.0.0 release packages, downloaded and sha256-checked against the fixture "
                    "table; the receipts bind them to this run's cells only (no build provenance); "
                    "`simulated: false` and the `success` publication inputs are synthetic.\n\n")
            f.write("certification_state=`%s` execution_status=`%s` test_verdict=`%s` coverage_status=`%s`\n\n"
                    % tuple(out.get(k) for k in ("certification_state", "execution_status", "test_verdict", "coverage_status")))
            f.write("Coverage from evidence (a leg is tested-pass only with its plan entry, result leg, summary "
                    "artifact and leg job log, certificate evidence included, all as expected):\n\n")
            f.write("| detector cell | container | capture | PG16 | PG17 | PG18 |\n|---|---|---|---|---|---|\n")
            for r in rows:
                f.write("| %s |\n" % " | ".join(r))
            f.write("\n**%s**\n" % ("PASS" if not problems else "FAIL: %d problem(s)" % len(problems)))
            for p in problems:
                f.write("- %s\n" % p)
    print("\n".join(problems) if problems else "chain evidence as expected")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
