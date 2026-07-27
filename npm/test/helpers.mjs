// Test helpers: deterministic stub executables and fake npm install trees so
// launcher tests never need the real Go binary or the network.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const npmRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
);

// A stand-in for the native gskill binary. Records argv as JSON when argvFile
// is set, prints stdoutText, then exits with exitCode or kills itself with signal.
export function makeStub(file, { exitCode = 0, signal = null, argvFile = null, stdoutText = null } = {}) {
  const config = JSON.stringify({ exitCode, signal, argvFile, stdoutText });
  const script = [
    "#!/usr/bin/env node",
    `const cfg = ${config};`,
    'const fs = require("node:fs");',
    "const args = process.argv.slice(2);",
    "if (cfg.argvFile) fs.writeFileSync(cfg.argvFile, JSON.stringify(args));",
    "if (cfg.stdoutText) process.stdout.write(cfg.stdoutText);",
    "if (cfg.signal) { process.kill(process.pid, cfg.signal); setTimeout(() => {}, 5000); }",
    "else process.exit(cfg.exitCode);",
    "",
  ].join("\n");
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, script, { mode: 0o755 });
  return file;
}

// Creates node_modules/<npmName>/{package.json,bin/gskill} with a stub binary.
export function makePlatformPackage(rootDir, npmName, version, stubOpts = {}) {
  const pkgDir = path.join(rootDir, "node_modules", ...npmName.split("/"));
  fs.mkdirSync(pkgDir, { recursive: true });
  fs.writeFileSync(
    path.join(pkgDir, "package.json"),
    JSON.stringify({ name: npmName, version }, null, 2),
  );
  makeStub(path.join(pkgDir, "bin", "gskill"), stubOpts);
  return pkgDir;
}

// Copies the real launcher package sources into node_modules/@glapsfun/gskill
// so resolution walks the same layout npm produces.
export function installLauncher(rootDir) {
  const dest = path.join(rootDir, "node_modules", "@glapsfun", "gskill");
  fs.mkdirSync(dest, { recursive: true });
  for (const entry of ["package.json", "bin", "lib"]) {
    fs.cpSync(path.join(npmRoot, entry), path.join(dest, entry), { recursive: true });
  }
  return path.join(dest, "bin", "gskill.js");
}
