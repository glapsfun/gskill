import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { buildPackages } from "../scripts/build-packages.mjs";
import { targets } from "../lib/platforms.js";
import { makeArchive, makeArchiveSet } from "./fixtures/make-fixtures.mjs";

const VERSION = "1.2.3";

function tmp(name) {
  return fs.mkdtempSync(path.join(os.tmpdir(), `gskill-build-${name}-`));
}

function distName(t) {
  return t.npmName.split("/")[1];
}

test("valid archive set produces one launcher and four platform packages", () => {
  const archives = tmp("arch");
  const out = path.join(tmp("out"), "dist");
  makeArchiveSet(archives, VERSION);

  buildPackages({ version: VERSION, archivesDir: archives, outDir: out });

  const entries = fs.readdirSync(out).sort();
  assert.deepEqual(entries, ["gskill", ...targets.map(distName)].sort());

  for (const t of targets) {
    const dir = path.join(out, distName(t));
    const manifest = JSON.parse(fs.readFileSync(path.join(dir, "package.json"), "utf8"));
    assert.equal(manifest.name, t.npmName);
    assert.equal(manifest.version, VERSION);
    assert.deepEqual(manifest.os, [t.nodePlatform]);
    assert.deepEqual(manifest.cpu, [t.nodeArch]);
    assert.equal(manifest.publishConfig.provenance, true);
    assert.equal(manifest.scripts, undefined);
    assert.equal(manifest.dependencies, undefined);

    const bin = path.join(dir, "bin", "gskill");
    const mode = fs.statSync(bin).mode & 0o777;
    assert.equal(mode & 0o111, 0o111, `${bin} must be executable`);
    assert.ok(fs.existsSync(path.join(dir, "LICENSE")), "platform package ships LICENSE");
  }

  const launcher = JSON.parse(
    fs.readFileSync(path.join(out, "gskill", "package.json"), "utf8"),
  );
  assert.equal(launcher.name, "@glapsfun/gskill");
  assert.equal(launcher.version, VERSION);
  for (const t of targets) {
    assert.equal(launcher.optionalDependencies[t.npmName], VERSION);
  }
  assert.ok(fs.existsSync(path.join(out, "gskill", "bin", "gskill.js")));
  assert.ok(fs.existsSync(path.join(out, "gskill", "lib", "platforms.js")));
  assert.ok(fs.existsSync(path.join(out, "gskill", "LICENSE")));
});

test("missing archive fails naming it and produces nothing", () => {
  const archives = tmp("miss");
  const out = path.join(tmp("missout"), "dist");
  for (const t of targets.slice(1)) makeArchive(archives, t, VERSION);

  assert.throws(
    () => buildPackages({ version: VERSION, archivesDir: archives, outDir: out }),
    new RegExp(`missing archive.*gskill_${VERSION}_darwin_amd64\\.tar\\.gz`),
  );
  assert.ok(!fs.existsSync(out), "no output on failure");
});

test("corrupt archive fails naming it and produces nothing", () => {
  const archives = tmp("corrupt");
  const out = path.join(tmp("corruptout"), "dist");
  makeArchiveSet(archives, VERSION);
  makeArchive(archives, targets[2], VERSION, "corrupt");

  assert.throws(
    () => buildPackages({ version: VERSION, archivesDir: archives, outDir: out }),
    new RegExp(`malformed archive.*gskill_${VERSION}_linux_amd64\\.tar\\.gz`),
  );
  assert.ok(!fs.existsSync(out));
});

test("archive without a gskill entry fails and produces nothing", () => {
  const archives = tmp("nobin");
  const out = path.join(tmp("nobinout"), "dist");
  makeArchiveSet(archives, VERSION);
  makeArchive(archives, targets[1], VERSION, "no-binary");

  assert.throws(
    () => buildPackages({ version: VERSION, archivesDir: archives, outDir: out }),
    /gskill binary.*gskill_1\.2\.3_darwin_arm64\.tar\.gz/,
  );
  assert.ok(!fs.existsSync(out));
});

test("non-executable gskill entry fails and produces nothing", () => {
  const archives = tmp("noexec");
  const out = path.join(tmp("noexecout"), "dist");
  makeArchiveSet(archives, VERSION);
  makeArchive(archives, targets[3], VERSION, "non-executable");

  assert.throws(
    () => buildPackages({ version: VERSION, archivesDir: archives, outDir: out }),
    /not executable.*gskill_1\.2\.3_linux_arm64\.tar\.gz/,
  );
  assert.ok(!fs.existsSync(out));
});

test("invalid version is rejected before touching archives", () => {
  assert.throws(
    () => buildPackages({ version: "v1.2.3", archivesDir: "/nope", outDir: "/nope2" }),
    /invalid version/,
  );
  assert.throws(
    () => buildPackages({ version: "1.2", archivesDir: "/nope", outDir: "/nope2" }),
    /invalid version/,
  );
});

test("non-empty output directory is refused", () => {
  const archives = tmp("dirty");
  const out = tmp("dirtyout");
  fs.writeFileSync(path.join(out, "stale"), "x");
  makeArchiveSet(archives, VERSION);
  assert.throws(
    () => buildPackages({ version: VERSION, archivesDir: archives, outDir: out }),
    /not empty/,
  );
});

test("builder source performs no network access", () => {
  const src = fs.readFileSync(
    path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../scripts/build-packages.mjs"),
    "utf8",
  );
  assert.doesNotMatch(src, /node:https?|fetch\s*\(|XMLHttpRequest|WebSocket/);
});
