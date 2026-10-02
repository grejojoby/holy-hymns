import { spawnSync } from "node:child_process";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  advisory,
  mobileRoot,
  reviewBefore,
  verifyPatch,
} from "./node-forge-patch.mjs";

export function auditBlockers(report, patchedPaths, now = new Date()) {
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
  const allowed = (name, seen = new Set()) => {
    const item = vulnerabilities[name];
    if (
      !item ||
      item.severity !== "high" ||
      seen.has(name) ||
      !Array.isArray(item.via) ||
      !item.via.length
    )
      return false;
    const next = new Set([...seen, name]);
    return item.via.every((cause) => {
      if (typeof cause === "string") return allowed(cause, next);
      return (
        name === "node-forge" &&
        cause?.name === "node-forge" &&
        cause.dependency === "node-forge" &&
        cause.url === advisory &&
        cause.range === "<=1.4.0" &&
        cause.severity === "high" &&
        Array.isArray(item.nodes) &&
        item.nodes.length > 0 &&
        item.nodes.every((path) => patchedPaths.includes(path))
      );
    });
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
  const blockers = auditBlockers(report, paths);
  if (blockers.length) {
    console.error(JSON.stringify(report, null, 2));
    throw new Error(
      `Unmitigated high/critical vulnerabilities: ${blockers.join(", ")}`,
    );
  }
  console.log(
    "Dependency audit passed: no unmitigated high/critical vulnerabilities.",
  );
  if (report.vulnerabilities["node-forge"]) {
    console.log(
      `Mitigated ${advisory}: reviewed backport and strict DER validation verified; review before ${reviewBefore}.`,
    );
  }
}
