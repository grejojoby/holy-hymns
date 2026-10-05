import { createHash } from "node:crypto";
import { readFileSync, realpathSync, writeFileSync } from "node:fs";
import { dirname, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const directory = dirname(fileURLToPath(import.meta.url));
const mobileRoot = resolve(directory, "..");
const patch = JSON.parse(
  readFileSync(resolve(directory, "braces-patch.json"), "utf8"),
);
export const advisory = "https://github.com/advisories/GHSA-vfj7-8cjw-p6xm";
export const reviewBefore = "2026-10-16T00:00:00Z";
const sha256 = (value) => createHash("sha256").update(value).digest("hex");

export function patchSource(file, source) {
  const expected = patch.files[file];
  if (!expected) throw new Error("Unrecognized braces file");
  if (sha256(source) === expected.patchedHash) return source;
  if (sha256(source) !== expected.originalHash)
    throw new Error(`Unrecognized braces source: ${file}`);
  let result = source;
  for (const { before, after } of expected.changes) {
    if (result.split(before).length !== 2)
      throw new Error(`Ambiguous braces patch: ${file}`);
    result = result.replace(before, () => after);
  }
  if (sha256(result) !== expected.patchedHash)
    throw new Error(`Braces backport differs from pinned upstream: ${file}`);
  return result;
}

export function verifyPatch({ apply = false, root = mobileRoot } = {}) {
  const lock = JSON.parse(
    readFileSync(resolve(root, "package-lock.json"), "utf8"),
  );
  const paths = Object.keys(lock.packages).filter((path) =>
    path.endsWith("/braces"),
  );
  if (!paths.length)
    throw new Error("braces dependency changed; review the backport");
  const pending = [];
  for (const path of paths) {
    const installed = realpathSync(resolve(root, path));
    if (
      !installed.startsWith(realpathSync(resolve(root, "node_modules")) + sep)
    )
      throw new Error(
        "braces must be installed inside this project's node_modules",
      );
    const manifest = JSON.parse(
      readFileSync(resolve(installed, "package.json"), "utf8"),
    );
    if (
      lock.packages[path].version !== "3.0.3" ||
      manifest.name !== "braces" ||
      manifest.version !== "3.0.3"
    )
      throw new Error(
        "braces version changed; review upstream fix availability",
      );
    for (const [relative, expected] of Object.entries(patch.files)) {
      const file = realpathSync(resolve(installed, relative));
      if (!file.startsWith(installed + sep))
        throw new Error("braces source is outside its package");
      const source = readFileSync(file, "utf8");
      if (sha256(source) === expected.patchedHash) continue;
      if (!apply)
        throw new Error(
          `Security backport missing or changed: ${path}/${relative}`,
        );
      pending.push([file, patchSource(relative, source)]);
    }
  }
  // Verify all files before modifying any installed copy.
  for (const [file, source] of pending) writeFileSync(file, source);
  return paths;
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const paths = verifyPatch({ apply: !process.argv.includes("--check") });
  console.log(
    `Verified braces depth-limit backport in ${paths.length} installed copy/copies.`,
  );
}
