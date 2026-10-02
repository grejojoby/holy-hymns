import { createHash } from "node:crypto";
import { readFileSync, realpathSync, writeFileSync } from "node:fs";
import { dirname, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

export const mobileRoot = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "..",
);
export const advisory = "https://github.com/advisories/GHSA-86w9-cpqp-85rv";
export const reviewBefore = "2026-10-16T00:00:00Z";
// lib/rsa.js from npm node-forge 1.4.0 and upstream proposed child-count fix:
// https://github.com/digitalbazaar/forge/pull/1152
// commit ceba34402e329f0365134f23fe19898756527d65 (not yet released).
export const originalHash =
  "fd4740238145ec26470eb3f06a627c72039538ce1307dbdce40521f94dfd0a50";
const upstreamHash =
  "acc22e5d36e27832c34e02dd3933aad7977d45b047eead5016520735efedc9c5";
// Also enforce canonical DER: the proposal alone accepts garbage inside NULL/OID.
// RFC 8017 section 9.2 requires DER-encoded DigestInfo (see docs/mobile-security.md).
export const patchedHash =
  "3d891c4862ec4edc794adfa3dddc6c5fd6535f85902918adaf90b2f3dac5bb70";
const sha256 = (text) => createHash("sha256").update(text).digest("hex");

export function patchSource(source) {
  if (sha256(source) === patchedHash) return source;
  if (![originalHash, upstreamHash].includes(sha256(source)))
    throw new Error("Unrecognized node-forge RSA source; review the backport");
  const patched = source
    .replace(
      "          // validate DigestInfo structure and element count\n",
      "          // validate DigestInfo structure and element counts (outer DigestInfo\n" +
        "          // and nested DigestAlgorithm). asn1.validate ignores extra children,\n" +
        "          // so length must be checked explicitly at each nesting level to\n" +
        "          // prevent low-exponent PKCS#1 v1.5 signature forgery (CVE-2026-85393).\n",
    )
    .replace(
      "            obj.value.length !== 2) {",
      "            obj.value.length !== 2 ||\n" +
        "            obj.value[0].value.length !==\n" +
        "              (('parameters' in capture) ? 2 : 1)) {",
    )
    .replace(
      "              (('parameters' in capture) ? 2 : 1)) {",
      "              (('parameters' in capture) ? 2 : 1) ||\n" +
        "            // Local hardening: require canonical DER, including primitive values.\n" +
        "            (('parameters' in capture) && capture.parameters !== '') ||\n" +
        "            asn1.oidToDer(asn1.derToOid(capture.algorithmIdentifier)).getBytes() !==\n" +
        "              capture.algorithmIdentifier ||\n" +
        "            asn1.toDer(obj).getBytes() !== d) {",
    );
  if (sha256(patched) !== patchedHash)
    throw new Error("Backport differs from reviewed source");
  return patched;
}

export function verifyPatch({ apply = false, root = mobileRoot } = {}) {
  const lock = JSON.parse(
    readFileSync(resolve(root, "package-lock.json"), "utf8"),
  );
  const paths = Object.keys(lock.packages).filter((path) =>
    path.endsWith("/node-forge"),
  );
  if (!paths.length)
    throw new Error(
      "node-forge dependency changed; remove or review the backport",
    );
  for (const path of paths) {
    const directory = realpathSync(resolve(root, path));
    if (
      !directory.startsWith(realpathSync(resolve(root, "node_modules")) + sep)
    ) {
      throw new Error(
        "node-forge must be installed inside this project's node_modules",
      );
    }
    const manifest = JSON.parse(
      readFileSync(resolve(directory, "package.json"), "utf8"),
    );
    if (
      lock.packages[path].version !== "1.4.0" ||
      manifest.name !== "node-forge" ||
      manifest.version !== "1.4.0"
    ) {
      throw new Error(
        "node-forge version changed; review upstream fix availability",
      );
    }
    const file = realpathSync(resolve(directory, "lib/rsa.js"));
    if (!file.startsWith(directory + sep))
      throw new Error("RSA source is outside its installed package");
    const source = readFileSync(file, "utf8");
    if (apply && sha256(source) !== patchedHash)
      writeFileSync(file, patchSource(source));
    if (sha256(readFileSync(file)) !== patchedHash)
      throw new Error(`Security backport missing or changed: ${path}`);
  }
  return paths;
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const paths = verifyPatch({ apply: !process.argv.includes("--check") });
  console.log(
    `Verified node-forge security backport in ${paths.length} installed copy/copies.`,
  );
}
