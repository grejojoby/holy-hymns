import { spawnSync } from "node:child_process";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  advisory,
  mobileRoot,
  reviewBefore,
  verifyPatch,
} from "./node-forge-patch.mjs";

import {
  advisory as bracesAdvisory,
  reviewBefore as bracesReviewBefore,
  verifyPatch as verifyBracesPatch,
} from "./braces-patch.mjs";

export function auditBlockers(
  report,
  patchedPaths,
  now = new Date(),
  bracesPaths = [],
) {
  if (
    !report ||
    report.error ||
    report.auditReportVersion !== 2 ||
    !report.vulnerabilities ||
    typeof report.vulnerabilities !== "object" ||
    Array.isArray(report.vulnerabilities) ||
    report.metadata?.vulnerabilities?.total !==
      Object.keys(report.vulnerabilities).length
  ) {
    throw new Error(
      "Invalid npm audit report; refusing to pass security checks",
    );
  }
  if (now >= new Date(reviewBefore))
    throw new Error(
      "node-forge mitigation review is due; check for an upstream release",
    );
  if (now >= new Date(bracesReviewBefore))
    throw new Error(
      "braces mitigation review is due; check for an upstream release",
    );
  const vulnerabilities = report.vulnerabilities;
  for (const severity of ["info", "low", "moderate", "high", "critical"]) {
    if (
      report.metadata.vulnerabilities[severity] !==
      Object.values(vulnerabilities).filter(
        (item) => item?.severity === severity,
      ).length
    ) {
      throw new Error("Inconsistent npm audit severity counts");
    }
  }
  const mitigations = {
    "node-forge": { url: advisory, range: "<=1.4.0", paths: patchedPaths },
    braces: { url: bracesAdvisory, range: "<=3.0.3", paths: bracesPaths },
  };
  const allowed = (name) => {
    // npm's Metro dependency graph contains cycles. Traverse every reachable
    // cause once; require a verified advisory root and reject any unknown root.
    const pending = [name];
    const seen = new Set();
    const verifiedRoots = new Set();
    const parents = new Map();
    while (pending.length) {
      const current = pending.pop();
      if (seen.has(current)) continue;
      seen.add(current);
      const item = vulnerabilities[current];
      if (
        !item ||
        item.severity !== "high" ||
        !Array.isArray(item.via) ||
        !item.via.length
      )
        return false;
      for (const cause of item.via) {
        if (typeof cause === "string") {
          if (!parents.has(cause)) parents.set(cause, []);
          parents.get(cause).push(current);
          pending.push(cause);
          continue;
        }
        const mitigation = mitigations[current];
        if (
          !mitigation ||
          cause?.name !== current ||
          cause.dependency !== current ||
          cause.url !== mitigation.url ||
          cause.range !== mitigation.range ||
          cause.severity !== "high" ||
          !Array.isArray(item.nodes) ||
          !item.nodes.length ||
          !item.nodes.every((path) => mitigation.paths.includes(path))
        )
          return false;
        verifiedRoots.add(current);
      }
    }
    const reachable = [...verifiedRoots];
    for (let i = 0; i < reachable.length; i++) {
      for (const parent of parents.get(reachable[i]) || []) {
        if (!verifiedRoots.has(parent)) {
          verifiedRoots.add(parent);
          reachable.push(parent);
        }
      }
    }
    return [...seen].every((node) => verifiedRoots.has(node));
  };
  return Object.entries(vulnerabilities)
    .filter(([name, item]) => {
      if (
        !item ||
        !["info", "low", "moderate", "high", "critical"].includes(item.severity)
      ) {
        throw new Error(`Invalid advisory severity for ${name}`);
      }
      return ["high", "critical"].includes(item.severity) && !allowed(name);
    })
    .map(([name]) => name);
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const paths = verifyPatch();
  const bracesPaths = verifyBracesPatch();
  const result = spawnSync(
    process.platform === "win32" ? "npm.cmd" : "npm",
    [
      "audit",
      "--omit=dev",
      "--audit-level=high",
      "--json",
      "--registry=https://registry.npmjs.org",
    ],
    {
      cwd: mobileRoot,
      encoding: "utf8",
      timeout: 120_000,
      maxBuffer: 10 * 1024 * 1024,
    },
  );
  if (result.error || ![0, 1].includes(result.status))
    throw new Error("npm audit failed to complete", { cause: result.error });
  const report = JSON.parse(result.stdout);
  const reportedHigh = report.metadata?.vulnerabilities?.high;
  const reportedCritical = report.metadata?.vulnerabilities?.critical;
  if (
    !Number.isInteger(reportedHigh) ||
    !Number.isInteger(reportedCritical) ||
    reportedHigh < 0 ||
    reportedCritical < 0 ||
    (result.status === 1 && reportedHigh + reportedCritical === 0)
  ) {
    throw new Error(
      "npm audit status/counts are inconsistent; refusing to pass security checks",
    );
  }
  const blockers = auditBlockers(report, paths, new Date(), bracesPaths);
  if (blockers.length) {
    console.error(JSON.stringify(report, null, 2));
    throw new Error(
      `Unmitigated high/critical vulnerabilities: ${blockers.join(", ")}`,
    );
  }
  console.log(
    "Dependency audit passed: no unmitigated high/critical vulnerabilities.",
  );
  if (report.vulnerabilities.braces) {
    console.log(
      `Mitigated ${bracesAdvisory}: pinned depth-limit backport verified; review before ${bracesReviewBefore}.`,
    );
  }
  if (report.vulnerabilities["node-forge"]) {
    console.log(
      `Mitigated ${advisory}: reviewed backport and strict DER validation verified; review before ${reviewBefore}.`,
    );
  }
}
