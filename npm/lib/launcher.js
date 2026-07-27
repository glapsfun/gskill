// Transparent trampoline to the native gskill binary. Contract:
// specs/019-npm-distribution/contracts/launcher-behavior.md — argv unchanged,
// stdio inherited, no shell, native exit code, fail closed, never download.
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";

import { lookup, supportedMatrix } from "./platforms.js";

const requireFromHere = createRequire(import.meta.url);

export function unsupportedMessage(platform, arch) {
  return [
    `gskill: unsupported platform ${platform}/${arch}.`,
    `gskill ships native binaries for: ${supportedMatrix}.`,
    "See https://github.com/glapsfun/gskill#install for supported install methods.",
  ].join("\n");
}

export function missingPackageMessage(npmName) {
  return [
    `gskill: platform package ${npmName} is not installed.`,
    "This usually means optional dependencies were disabled. Reinstall with:",
    "  npm install @glapsfun/gskill",
    "(or rerun npx without --no-optional / omit=optional configuration)",
  ].join("\n");
}

export function resolveBinary(npmName, resolve = requireFromHere.resolve) {
  const pkgJson = resolve(`${npmName}/package.json`);
  return path.join(path.dirname(pkgJson), "bin", "gskill");
}

export function main({
  platform = process.platform,
  arch = process.arch,
  argv = process.argv.slice(2),
  spawn = spawnSync,
  resolve = requireFromHere.resolve,
  exists = fs.existsSync,
  error = (msg) => process.stderr.write(`${msg}\n`),
  raise = (sig) => process.kill(process.pid, sig),
} = {}) {
  const target = lookup(platform, arch);
  if (!target) {
    error(unsupportedMessage(platform, arch));
    return 1;
  }

  let binary;
  try {
    binary = resolveBinary(target.npmName, resolve);
  } catch {
    error(missingPackageMessage(target.npmName));
    return 1;
  }
  if (!exists(binary)) {
    error(missingPackageMessage(target.npmName));
    return 1;
  }

  const result = spawn(binary, argv, { stdio: "inherit" });
  if (result.error) {
    error(`gskill: failed to start ${binary}: ${result.error.message}`);
    return 1;
  }
  if (result.signal) {
    raise(result.signal);
    return 128;
  }
  return result.status ?? 1;
}
