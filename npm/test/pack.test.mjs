import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { buildPackages } from "../scripts/build-packages.mjs";
import { targets } from "../lib/platforms.js";
import { makeArchiveSet } from "./fixtures/make-fixtures.mjs";

const VERSION = "2.0.0";

function packFiles(dir) {
  const res = spawnSync("npm", ["pack", "--dry-run", "--json"], {
    cwd: dir,
    encoding: "utf8",
  });
  assert.equal(res.status, 0, `npm pack --dry-run failed in ${dir}: ${res.stderr}`);
  const [report] = JSON.parse(res.stdout);
  return report.files.map((f) => f.path).sort();
}

test("npm pack --dry-run succeeds for all five packages with the contracted files", () => {
  const archives = fs.mkdtempSync(path.join(os.tmpdir(), "gskill-pack-arch-"));
  const out = path.join(fs.mkdtempSync(path.join(os.tmpdir(), "gskill-pack-out-")), "dist");
  makeArchiveSet(archives, VERSION);
  buildPackages({ version: VERSION, archivesDir: archives, outDir: out });

  const launcherFiles = packFiles(path.join(out, "gskill"));
  assert.deepEqual(launcherFiles, [
    "LICENSE",
    "README.md",
    "bin/gskill.js",
    "lib/launcher.js",
    "lib/platforms.js",
    "package.json",
  ]);

  for (const t of targets) {
    const files = packFiles(path.join(out, t.npmName.split("/")[1]));
    assert.deepEqual(files, ["LICENSE", "bin/gskill", "package.json"]);
  }
});
