import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { buildPackages } from "../scripts/build-packages.mjs";
import { targets, versionFromTag } from "../lib/platforms.js";
import { makeArchiveSet } from "./fixtures/make-fixtures.mjs";
import { npmRoot } from "./helpers.mjs";

const rangeOperators = /[\^~><*x ]|\|\|/;

function buildAt(version) {
  const archives = fs.mkdtempSync(path.join(os.tmpdir(), "gskill-ver-arch-"));
  const out = path.join(fs.mkdtempSync(path.join(os.tmpdir(), "gskill-ver-out-")), "dist");
  makeArchiveSet(archives, version);
  buildPackages({ version, archivesDir: archives, outDir: out });
  return out;
}

function readManifest(dir) {
  return JSON.parse(fs.readFileSync(path.join(dir, "package.json"), "utf8"));
}

for (const version of ["4.5.6", "0.6.0-rc.1"]) {
  test(`build at ${version} stamps all five packages to exactly that version`, () => {
    const out = buildAt(version);
    const launcher = readManifest(path.join(out, "gskill"));
    assert.equal(launcher.version, version);

    const deps = launcher.optionalDependencies;
    assert.deepEqual(Object.keys(deps).sort(), targets.map((t) => t.npmName).sort());
    for (const [name, dep] of Object.entries(deps)) {
      assert.equal(dep, version, `${name} must pin the exact version`);
      assert.doesNotMatch(dep, rangeOperators, `${name} must not use a range`);
    }

    for (const t of targets) {
      const manifest = readManifest(path.join(out, t.npmName.split("/")[1]));
      assert.equal(manifest.version, version);
    }
  });
}

test("committed launcher manifest stays at the unpublishable dev placeholder", () => {
  const manifest = JSON.parse(fs.readFileSync(path.join(npmRoot, "package.json"), "utf8"));
  assert.equal(manifest.version, "0.0.0-dev");
  for (const dep of Object.values(manifest.optionalDependencies)) {
    assert.equal(dep, "0.0.0-dev");
  }
});

test("tag to version derivation strips exactly one leading v", () => {
  assert.equal(versionFromTag("v4.5.6"), "4.5.6");
  assert.throws(() => versionFromTag("vv4.5.6"));
  assert.throws(() => versionFromTag("4.5.6"));
});
