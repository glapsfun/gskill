import { test } from "node:test";
import assert from "node:assert/strict";

import {
  archiveName,
  lookup,
  supportedMatrix,
  targets,
  versionFromTag,
} from "../lib/platforms.js";

const expected = [
  ["darwin", "x64", "darwin", "amd64", "@glapsfun/gskill-darwin-x64"],
  ["darwin", "arm64", "darwin", "arm64", "@glapsfun/gskill-darwin-arm64"],
  ["linux", "x64", "linux", "amd64", "@glapsfun/gskill-linux-x64"],
  ["linux", "arm64", "linux", "arm64", "@glapsfun/gskill-linux-arm64"],
];

test("exactly the four supported targets exist", () => {
  assert.equal(targets.length, 4);
  const names = targets.map((t) => t.npmName);
  assert.deepEqual(new Set(names).size, 4);
});

for (const [nodePlatform, nodeArch, goOs, goArch, npmName] of expected) {
  test(`${nodePlatform}/${nodeArch} maps to ${npmName}`, () => {
    const t = lookup(nodePlatform, nodeArch);
    assert.ok(t, "supported pair must resolve");
    assert.equal(t.goOs, goOs);
    assert.equal(t.goArch, goArch);
    assert.equal(t.npmName, npmName);
  });
}

test("archive name matches the goreleaser contract (no v prefix)", () => {
  const t = lookup("darwin", "x64");
  assert.equal(archiveName(t, "0.5.0"), "gskill_0.5.0_darwin_amd64.tar.gz");
  const l = lookup("linux", "arm64");
  assert.equal(archiveName(l, "0.6.0-rc.1"), "gskill_0.6.0-rc.1_linux_arm64.tar.gz");
});

test("unsupported pairs miss without fallback", () => {
  for (const [p, a] of [
    ["win32", "x64"],
    ["win32", "arm64"],
    ["linux", "ia32"],
    ["sunos", "x64"],
    ["darwin", "ppc64"],
    ["", ""],
  ]) {
    assert.equal(lookup(p, a), undefined, `${p}/${a} must not resolve`);
  }
});

test("supported matrix names all four pairs for diagnostics", () => {
  assert.equal(supportedMatrix, "darwin/x64, darwin/arm64, linux/x64, linux/arm64");
});

test("versionFromTag strips exactly one leading v and validates shape", () => {
  assert.equal(versionFromTag("v0.5.0"), "0.5.0");
  assert.equal(versionFromTag("v0.6.0-rc.1"), "0.6.0-rc.1");
  assert.equal(versionFromTag("v10.20.30"), "10.20.30");
  for (const bad of ["0.5.0", "vv0.5.0", "v0.5", "v0.5.0.1", "main", "v0.5.0 ", ""]) {
    assert.throws(() => versionFromTag(bad), /release tag/, `must reject ${JSON.stringify(bad)}`);
  }
});
