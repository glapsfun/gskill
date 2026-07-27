// Turns the four GoReleaser release archives into the four platform npm
// packages plus the version-stamped launcher, in <outDir>. Contract:
// specs/019-npm-distribution/contracts/npm-packages.md. Archives must already
// be checksum-verified by the caller; this builder never touches the network.
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { parseArgs } from "node:util";

import { archiveName, targets, versionPattern } from "../lib/platforms.js";

const npmRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function fail(msg) {
  throw new Error(msg);
}

function extractArchive(archive, dest) {
  fs.mkdirSync(dest, { recursive: true });
  const tar = spawnSync("tar", ["-xzf", archive, "-C", dest]);
  if (tar.status !== 0) {
    fail(`malformed archive (tar.gz extraction failed): ${archive}`);
  }
}

function platformManifest(target, version) {
  const [platform, arch] = [target.nodePlatform, target.nodeArch];
  return {
    name: target.npmName,
    version,
    description: `gskill native binary for ${platform}/${arch} (internal dependency of @glapsfun/gskill)`,
    os: [platform],
    cpu: [arch],
    files: ["bin/"],
    engines: { node: ">=20" },
    repository: { type: "git", url: "git+https://github.com/glapsfun/gskill.git" },
    license: "MIT",
    publishConfig: { access: "public", provenance: true },
  };
}

function writeManifest(dir, manifest) {
  fs.writeFileSync(path.join(dir, "package.json"), `${JSON.stringify(manifest, null, 2)}\n`);
}

function buildPlatformPackage(target, version, archivesDir, stagingDir) {
  const archive = path.join(archivesDir, archiveName(target, version));
  if (!fs.existsSync(archive)) {
    fail(`missing archive: ${archive}`);
  }

  const extracted = fs.mkdtempSync(path.join(os.tmpdir(), "gskill-extract-"));
  try {
    extractArchive(archive, extracted);
    const binary = path.join(extracted, "gskill");
    if (!fs.existsSync(binary) || !fs.statSync(binary).isFile()) {
      fail(`no gskill binary in archive: ${archive}`);
    }
    if ((fs.statSync(binary).mode & 0o111) === 0) {
      fail(`gskill binary is not executable in archive: ${archive}`);
    }

    const pkgDir = path.join(stagingDir, target.npmName.split("/")[1]);
    fs.mkdirSync(path.join(pkgDir, "bin"), { recursive: true });
    fs.copyFileSync(binary, path.join(pkgDir, "bin", "gskill"));
    fs.chmodSync(path.join(pkgDir, "bin", "gskill"), 0o755);
    fs.copyFileSync(path.join(npmRoot, "LICENSE"), path.join(pkgDir, "LICENSE"));
    writeManifest(pkgDir, platformManifest(target, version));
  } finally {
    fs.rmSync(extracted, { recursive: true, force: true });
  }
}

function buildLauncherPackage(version, stagingDir) {
  const pkgDir = path.join(stagingDir, "gskill");
  fs.mkdirSync(pkgDir, { recursive: true });
  for (const entry of ["bin", "lib", "README.md", "LICENSE"]) {
    fs.cpSync(path.join(npmRoot, entry), path.join(pkgDir, entry), { recursive: true });
  }
  const manifest = JSON.parse(fs.readFileSync(path.join(npmRoot, "package.json"), "utf8"));
  manifest.version = version;
  for (const t of targets) {
    manifest.optionalDependencies[t.npmName] = version;
  }
  writeManifest(pkgDir, manifest);
}

export function buildPackages({ version, archivesDir, outDir }) {
  if (typeof version !== "string" || !versionPattern.test(version)) {
    fail(`invalid version ${JSON.stringify(version)} — expected X.Y.Z or X.Y.Z-<prerelease>`);
  }
  if (fs.existsSync(outDir) && fs.readdirSync(outDir).length > 0) {
    fail(`output directory is not empty: ${outDir}`);
  }

  const stagingDir = `${outDir}.staging-${process.pid}`;
  fs.rmSync(stagingDir, { recursive: true, force: true });
  fs.mkdirSync(stagingDir, { recursive: true });
  try {
    for (const target of targets) {
      buildPlatformPackage(target, version, archivesDir, stagingDir);
    }
    buildLauncherPackage(version, stagingDir);
  } catch (err) {
    fs.rmSync(stagingDir, { recursive: true, force: true });
    throw err;
  }

  fs.rmSync(outDir, { recursive: true, force: true });
  fs.renameSync(stagingDir, outDir);
  return outDir;
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? "").href) {
  try {
    const { values } = parseArgs({
      options: {
        version: { type: "string" },
        archives: { type: "string" },
        out: { type: "string" },
      },
    });
    if (!values.version || !values.archives || !values.out) {
      throw new Error("usage: build-packages.mjs --version X.Y.Z --archives <dir> --out <dir>");
    }
    buildPackages({ version: values.version, archivesDir: values.archives, outDir: values.out });
    console.log(`built 5 packages at ${values.out}`);
  } catch (err) {
    console.error(`build-packages: ${err.message}`);
    process.exit(1);
  }
}
