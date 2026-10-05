import assert from "node:assert/strict";
import { createRequire } from "node:module";
import test from "node:test";
import { patchSource, verifyPatch } from "./braces-patch.mjs";
const require = createRequire(import.meta.url);
const braces = require("braces");
const nested = (n, open = "{", close = "}") =>
  open.repeat(n) + "a,b" + close.repeat(n);

test("deep brace and parenthesis patterns are rejected before exhausting the stack", () => {
  for (const [open, close] of [
    ["{", "}"],
    ["(", ")"],
  ]) {
    for (const method of ["parse", "compile", "expand", "stringify"]) {
      assert.doesNotThrow(() => braces[method](nested(100, open, close)));
      for (const depth of [101, 4500])
        assert.throws(
          () => braces[method](nested(depth, open, close)),
          /exceeds max depth/,
        );
      for (const maxDepth of [101, Infinity, NaN])
        assert.throws(
          () => braces[method](nested(101, open, close), { maxDepth }),
          /exceeds max depth/,
        );
      assert.throws(
        () => braces[method](nested(2, open, close), { maxDepth: 1.5 }),
        /exceeds max depth/,
      );
    }
  }
});

test("caller-provided deep ASTs and cyclic parent chains are bounded", () => {
  for (const method of ["compile", "expand", "stringify"]) {
    let ast = { type: "text", value: "a" };
    for (let i = 0; i < 4500; i++) ast = { type: "brace", nodes: [ast] };
    const root = { type: "root", nodes: [ast] };
    assert.throws(() => braces[method](root), /exceeds max depth/);
  }
  const node = { type: "paren", nodes: [] };
  node.parent = node;
  assert.throws(() => braces.expand(node), /cycle/);
});

test("ordinary Metro globs, ranges and escaped literals retain their behavior", () => {
  assert.equal(
    braces.compile("src/**/*.{js,jsx,ts,tsx}"),
    "src/**/*.(js|jsx|ts|tsx)",
  );
  assert.deepEqual(braces.expand("file{1..3}.{js,ts}"), [
    "file1.js",
    "file1.ts",
    "file2.js",
    "file2.ts",
    "file3.js",
    "file3.ts",
  ]);
  const pattern = "src/{components,{lib,hooks}}/*.tsx";
  assert.equal(braces.stringify(braces.parse(pattern)), pattern);
  assert.deepEqual(
    require("micromatch")(["index.ts", "page.tsx", "notes.md"], "*.{ts,tsx}"),
    ["index.ts", "page.tsx"],
  );
  assert.doesNotThrow(() => braces.compile("\\{".repeat(150)));
});

test("installed braces patch is exact and rejects unknown source", () => {
  assert.ok(verifyPatch().length);
  assert.throws(
    () => patchSource("lib/parse.js", "unrecognized"),
    /Unrecognized/,
  );
});
