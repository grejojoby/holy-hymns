import assert from "node:assert/strict";
import { generateKeyPairSync } from "node:crypto";
import { createRequire } from "node:module";
import test from "node:test";
import { auditBlockers } from "./audit-security.mjs";
import { advisory, patchSource, verifyPatch } from "./node-forge-patch.mjs";

const require = createRequire(import.meta.url);
const forge = require("node-forge");
const paths = ["node_modules/node-forge"];
const now = new Date("2026-10-02T00:00:00Z");
const cause = {
  name: "node-forge",
  dependency: "node-forge",
  url: advisory,
  range: "<=1.4.0",
  severity: "high",
};
const report = () => ({
  auditReportVersion: 2,
  metadata: {
    vulnerabilities: {
      info: 0,
      low: 0,
      moderate: 0,
      high: 2,
      critical: 0,
      total: 2,
    },
  },
  vulnerabilities: {
    "node-forge": { severity: "high", nodes: [...paths], via: [cause] },
    expo: {
      severity: "high",
      nodes: ["node_modules/expo"],
      via: ["node-forge"],
    },
  },
});

test("RSA verification rejects nested DigestAlgorithm garbage but accepts valid SHA256 signatures", () => {
  const { privateKey: pem } = generateKeyPairSync("rsa", {
    modulusLength: 2048,
    publicExponent: 3,
    privateKeyEncoding: { type: "pkcs1", format: "pem" },
  });
  const key = forge.pki.privateKeyFromPem(pem);
  const publicKey = forge.pki.setRsaPublicKey(key.n, key.e);
  const digest = forge.md.sha256
    .create()
    .update("Holy Hymns security regression")
    .digest()
    .getBytes();
  const asn1 = forge.asn1;
  const node = (type, constructed, value) =>
    asn1.create(asn1.Class.UNIVERSAL, type, constructed, value);
  for (const includeNull of [false, true]) {
    for (const variant of [
      "valid",
      "extra-child",
      "null-content",
      "oid-padding",
      "nonminimal-length",
      "indefinite-length",
    ]) {
      if (!includeNull && variant === "null-content") continue;
      let oid = asn1.oidToDer(forge.pki.oids.sha256).getBytes();
      if (variant === "oid-padding") oid += "\x80".repeat(32);
      const algorithm = [node(asn1.Type.OID, false, oid)];
      if (includeNull)
        algorithm.push(
          node(
            asn1.Type.NULL,
            false,
            variant === "null-content" ? "unexpected NULL bytes" : "",
          ),
        );
      const garbage = variant === "extra-child";
      if (garbage)
        algorithm.push(
          node(asn1.Type.OCTETSTRING, false, "unconsumed attacker bytes"),
        );
      const info = node(asn1.Type.SEQUENCE, true, [
        node(asn1.Type.SEQUENCE, true, algorithm),
        node(asn1.Type.OCTETSTRING, false, digest),
      ]);
      let encoded = asn1.toDer(info).getBytes();
      if (variant === "nonminimal-length")
        encoded = encoded[0] + "\x81" + encoded.slice(1);
      if (variant === "indefinite-length")
        encoded = encoded[0] + "\x80" + encoded.slice(2) + "\x00\x00";
      const signature = key.sign(encoded, null);
      if (variant !== "valid")
        assert.throws(
          () => publicKey.verify(digest, signature),
          /DigestInfo/,
          variant,
        );
      else assert.equal(publicKey.verify(digest, signature), true);
    }
  }
});

test("installed mitigation is exact and patching unknown source fails closed", () => {
  assert.ok(verifyPatch().length);
  assert.throws(() => patchSource("unrecognized source"), /Unrecognized/);
});

test("Expo certificate validation and SHA256 code signing still work", () => {
  const expo = require("@expo/code-signing-certificates");
  const keyPair = expo.generateKeyPair();
  const certificate = expo.generateSelfSignedCodeSigningCertificate({
    keyPair,
    validityNotBefore: new Date(Date.now() - 60_000),
    validityNotAfter: new Date(Date.now() + 60_000),
    commonName: "Holy Hymns ephemeral test certificate",
  });
  expo.validateSelfSignedCertificate(certificate, keyPair);
  const signature = expo.signBufferRSASHA256AndVerify(
    keyPair.privateKey,
    certificate,
    Buffer.from("test manifest"),
  );
  assert.ok(signature.length);
  const csr = expo.generateCSR(keyPair, "Holy Hymns ephemeral CSR");
  assert.equal(csr.verify(), true);
});

test("audit permits only the patched advisory and its transitive effects", () => {
  assert.deepEqual(auditBlockers(report(), paths, now), []);
  assert.deepEqual(auditBlockers(report(), [], now), ["node-forge", "expo"]);
  const additional = report();
  additional.vulnerabilities["node-forge"].via.push({
    ...cause,
    url: "https://github.com/advisories/GHSA-new-issue",
  });
  assert.deepEqual(auditBlockers(additional, paths, now), [
    "node-forge",
    "expo",
  ]);
  const unrelated = report();
  unrelated.vulnerabilities.other = {
    severity: "critical",
    via: [{ ...cause, name: "other" }],
  };
  unrelated.metadata.vulnerabilities.total++;
  unrelated.metadata.vulnerabilities.critical++;
  assert.deepEqual(auditBlockers(unrelated, paths, now), ["other"]);
});

test("audit rejects expired mitigation, invalid reports, unknown paths and cyclic causes", () => {
  assert.throws(
    () => auditBlockers(report(), paths, new Date("2026-10-16")),
    /review is due/,
  );
  assert.throws(
    () => auditBlockers({ error: "network" }, paths, now),
    /Invalid/,
  );
  assert.throws(
    () =>
      auditBlockers({ auditReportVersion: 2, vulnerabilities: 42 }, paths, now),
    /Invalid/,
  );
  const inconsistent = report();
  inconsistent.metadata.vulnerabilities.high = 0;
  assert.throws(() => auditBlockers(inconsistent, paths, now), /Inconsistent/);
  const nested = report();
  nested.vulnerabilities["node-forge"].nodes.push(
    "node_modules/other/node_modules/node-forge",
  );
  assert.deepEqual(auditBlockers(nested, ["node_modules/node-forge"], now), [
    "node-forge",
    "expo",
  ]);
  const cycle = report();
  cycle.vulnerabilities.expo.via = ["expo"];
  assert.deepEqual(auditBlockers(cycle, paths, now), ["expo"]);
});
