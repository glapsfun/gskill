import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";

import { main, missingPackageMessage, resolveBinary } from "../lib/launcher.js";
import { lookup } from "../lib/platforms.js";
import { installLauncher, makePlatformPackage } from "./helpers.mjs";

function fakeDeps(overrides = {}) {
  const calls = { spawn: [], errors: [], raised: [] };
  const deps = {
    platform: "linux",
    arch: "x64",
    argv: [],
    resolve: (id) => `/fake/node_modules/${id}`,
    exists: () => true,
    spawn: (cmd, args, opts) => {
      calls.spawn.push({ cmd, args, opts });
      return { status: 0, signal: null };
    },
    error: (msg) => calls.errors.push(msg),
    raise: (sig) => calls.raised.push(sig),
    ...overrides,
  };
  return { deps, calls };
}

test("argv is forwarded byte-for-byte with inherited stdio and no shell", () => {
  const argv = ["add", "a b c", "--skill", "wéird – name", "--", "-x", "宇宙", ""];
  const { deps, calls } = fakeDeps({ argv });
  const code = main(deps);
  assert.equal(code, 0);
  assert.equal(calls.spawn.length, 1);
  const s = calls.spawn[0];
  assert.deepEqual(s.args, argv);
  assert.equal(s.opts.stdio, "inherit");
  assert.equal(s.opts.shell, undefined);
  assert.match(s.cmd, /bin\/gskill$/);
});

test("native exit code is returned unchanged", () => {
  const { deps } = fakeDeps({ spawn: () => ({ status: 7, signal: null }) });
  assert.equal(main(deps), 7);
});

test("child killed by a signal re-raises the same signal", () => {
  const { deps, calls } = fakeDeps({ spawn: () => ({ status: null, signal: "SIGTERM" }) });
  main(deps);
  assert.deepEqual(calls.raised, ["SIGTERM"]);
});

test("spawn failure yields a diagnostic and exit 1", () => {
  const { deps, calls } = fakeDeps({
    spawn: () => ({ error: new Error("EACCES"), status: null, signal: null }),
  });
  assert.equal(main(deps), 1);
  assert.equal(calls.errors.length, 1);
  assert.match(calls.errors[0], /failed to start/);
});

test("missing platform package fails with remedy and never spawns", () => {
  const { deps, calls } = fakeDeps({
    platform: "darwin",
    arch: "arm64",
    resolve: () => {
      throw new Error("MODULE_NOT_FOUND");
    },
  });
  assert.equal(main(deps), 1);
  assert.equal(calls.spawn.length, 0);
  const msg = calls.errors.join("\n");
  assert.match(msg, /@glapsfun\/gskill-darwin-arm64/);
  assert.match(msg, /npm install @glapsfun\/gskill/);
  assert.match(msg, /optional dependencies/i);
});

test("resolved package with a missing binary file also fails closed", () => {
  const { deps, calls } = fakeDeps({ exists: () => false });
  assert.equal(main(deps), 1);
  assert.equal(calls.spawn.length, 0);
  assert.match(calls.errors.join("\n"), /@glapsfun\/gskill-linux-x64/);
});

test("resolveBinary joins bin/gskill next to the resolved package.json", () => {
  const p = resolveBinary("@glapsfun/gskill-linux-x64", () => "/nm/@glapsfun/gskill-linux-x64/package.json");
  assert.equal(p, path.join("/nm/@glapsfun/gskill-linux-x64", "bin", "gskill"));
});

test("missingPackageMessage names the package and the reinstall remedy", () => {
  const msg = missingPackageMessage("@glapsfun/gskill-linux-arm64");
  assert.match(msg, /@glapsfun\/gskill-linux-arm64 is not installed/);
  assert.match(msg, /npm install @glapsfun\/gskill/);
});

// Unsupported platforms fail closed before any resolution or spawn (FR-007).
for (const [platform, arch] of [
  ["win32", "x64"],
  ["win32", "arm64"],
  ["linux", "ia32"],
  ["sunos", "x64"],
  ["darwin", "mystery"],
]) {
  test(`unsupported ${platform}/${arch} exits 1 before resolution or spawn`, () => {
    const { deps, calls } = fakeDeps({
      platform,
      arch,
      resolve: () => {
        throw new Error("resolve must not run for unsupported platforms");
      },
    });
    assert.equal(main(deps), 1);
    assert.equal(calls.spawn.length, 0);
    assert.equal(calls.errors.length, 1);
    const msg = calls.errors[0];
    assert.match(msg, new RegExp(`unsupported platform ${platform}/${arch}`));
    assert.match(msg, /darwin\/x64, darwin\/arm64, linux\/x64, linux\/arm64/);
    assert.match(msg, /github\.com\/glapsfun\/gskill#install/);
  });
}

// End-to-end through a real npm-shaped tree with the current platform's package.
const current = lookup(process.platform, process.arch);

test("e2e: launcher runs the platform binary transparently", { skip: !current }, () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "gskill-launcher-"));
  const argvFile = path.join(root, "argv.json");
  const launcherBin = installLauncher(root);
  makePlatformPackage(root, current.npmName, "0.0.0-dev", {
    exitCode: 3,
    argvFile,
    stdoutText: "stub-stdout-marker",
  });
  const argv = ["add", "github.com/o/r", "--skill", "a b", "--", "宇宙"];
  const res = spawnSync(process.execPath, [launcherBin, ...argv], { encoding: "utf8" });
  assert.equal(res.status, 3);
  assert.match(res.stdout, /stub-stdout-marker/);
  assert.deepEqual(JSON.parse(fs.readFileSync(argvFile, "utf8")), argv);
  fs.rmSync(root, { recursive: true, force: true });
});

test("e2e: launcher dies with the child's signal", { skip: !current }, () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "gskill-launcher-sig-"));
  const launcherBin = installLauncher(root);
  makePlatformPackage(root, current.npmName, "0.0.0-dev", { signal: "SIGTERM" });
  const res = spawnSync(process.execPath, [launcherBin], { encoding: "utf8" });
  assert.equal(res.signal, "SIGTERM");
  fs.rmSync(root, { recursive: true, force: true });
});

test("e2e: missing platform package produces the remedy diagnostic", { skip: !current }, () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "gskill-launcher-miss-"));
  const launcherBin = installLauncher(root);
  const res = spawnSync(process.execPath, [launcherBin, "version"], { encoding: "utf8" });
  assert.equal(res.status, 1);
  assert.match(res.stderr, new RegExp(current.npmName.replace(/[/@]/g, "\\$&")));
  assert.match(res.stderr, /npm install @glapsfun\/gskill/);
  fs.rmSync(root, { recursive: true, force: true });
});
