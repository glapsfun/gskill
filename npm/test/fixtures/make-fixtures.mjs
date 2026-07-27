// Deterministic release-shaped fixture archives, generated at test time so no
// binaries are ever committed. Layout mirrors the GoReleaser archives: the
// gskill binary plus LICENSE/README at the archive root.
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { archiveName, targets } from "../../lib/platforms.js";

function stubBinary(version, target) {
  return [
    "#!/bin/sh",
    `echo "gskill v${version} stub ${target.goOs}/${target.goArch}"`,
    "exit 0",
    "",
  ].join("\n");
}

// opts: mode "valid" | "no-binary" | "non-executable" | "corrupt"
export function makeArchive(dir, target, version, mode = "valid") {
  fs.mkdirSync(dir, { recursive: true });
  const file = path.join(dir, archiveName(target, version));
  if (mode === "corrupt") {
    fs.writeFileSync(file, "this is not gzip data at all");
    return file;
  }
  const stage = fs.mkdtempSync(path.join(os.tmpdir(), "gskill-fixture-"));
  fs.writeFileSync(path.join(stage, "LICENSE"), "MIT stub\n");
  fs.writeFileSync(path.join(stage, "README.md"), "stub\n");
  if (mode !== "no-binary") {
    const bin = path.join(stage, "gskill");
    fs.writeFileSync(bin, stubBinary(version, target), {
      mode: mode === "non-executable" ? 0o644 : 0o755,
    });
  }
  const tar = spawnSync("tar", ["-czf", file, "-C", stage, "."]);
  fs.rmSync(stage, { recursive: true, force: true });
  if (tar.status !== 0) {
    throw new Error(`fixture tar failed: ${tar.stderr}`);
  }
  return file;
}

export function makeArchiveSet(dir, version) {
  return targets.map((t) => makeArchive(dir, t, version));
}
