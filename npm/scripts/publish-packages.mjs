// Ordered, retry-safe npm publisher: platform packages first, launcher last,
// exact-version existence check before every publish (validate-and-skip, never
// overwrite), provenance mandatory. Contract:
// specs/019-npm-distribution/contracts/npm-packages.md.
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { parseArgs } from "node:util";

import { targets } from "../lib/platforms.js";

const LAUNCHER_NAME = "@glapsfun/gskill";

export function channelFor(version) {
  return version.includes("-") ? "next" : "latest";
}

// Semver precedence: numeric triplet, then dot-separated prerelease
// identifiers (numeric compare numerically and sort below alphanumeric).
export function compareVersions(a, b) {
  const parse = (v) => {
    const [core, ...pre] = v.split("-");
    return { nums: core.split(".").map(Number), pre: pre.join("-") };
  };
  const [pa, pb] = [parse(a), parse(b)];
  for (let i = 0; i < 3; i++) {
    if (pa.nums[i] !== pb.nums[i]) return pa.nums[i] < pb.nums[i] ? -1 : 1;
  }
  if (pa.pre === pb.pre) return 0;
  if (!pa.pre) return 1;
  if (!pb.pre) return -1;
  const [ids1, ids2] = [pa.pre.split("."), pb.pre.split(".")];
  for (let i = 0; i < Math.max(ids1.length, ids2.length); i++) {
    const [x, y] = [ids1[i], ids2[i]];
    if (x === undefined) return -1;
    if (y === undefined) return 1;
    if (x === y) continue;
    const [xNum, yNum] = [/^\d+$/.test(x), /^\d+$/.test(y)];
    if (xNum && yNum) return Number(x) < Number(y) ? -1 : 1;
    if (xNum !== yNum) return xNum ? -1 : 1;
    return x < y ? -1 : 1;
  }
  return 0;
}

function npmExec(cmd, args, opts = {}) {
  return spawnSync("npm", [cmd, ...args], { encoding: "utf8", ...opts });
}

// Real registry boundary; tests inject a fake with the same shape.
export const registryExec = {
  exists(name, version) {
    const res = npmExec("view", [`${name}@${version}`, "version", "--json"]);
    if (res.status === 0 && res.stdout.trim()) return true;
    if (/E404|404 Not Found/i.test(`${res.stderr}${res.stdout}`)) return false;
    if (res.status !== 0) {
      throw new Error(`npm view ${name}@${version} failed: ${res.stderr}`);
    }
    return false;
  },
  latestOf(name) {
    const res = npmExec("view", [name, "dist-tags.latest", "--json"]);
    if (res.status === 0 && res.stdout.trim()) return JSON.parse(res.stdout);
    // Only a proven never-published package may disable the downgrade guard;
    // any other failure (network, 5xx) must not be mistaken for "no latest".
    if (/E404|404 Not Found/i.test(`${res.stderr}${res.stdout}`)) return null;
    if (res.status !== 0) {
      throw new Error(`npm view ${name} dist-tags.latest failed: ${res.stderr}`);
    }
    return null;
  },
  publish(dir, name, tag) {
    const res = npmExec(
      "publish",
      ["--provenance", "--access", "public", "--tag", tag],
      { cwd: dir, stdio: ["ignore", "inherit", "inherit"], encoding: undefined },
    );
    if (res.status !== 0) {
      throw new Error(`npm publish ${name} failed with exit ${res.status}`);
    }
  },
};

function loadPackage(distDir, dirName, version) {
  const dir = path.join(distDir, dirName);
  const manifestPath = path.join(dir, "package.json");
  if (!fs.existsSync(manifestPath)) {
    throw new Error(`missing package in dist: ${dir}`);
  }
  const manifest = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
  if (manifest.version !== version) {
    throw new Error(
      `version mismatch in ${dir}: manifest has ${manifest.version}, expected ${version}`,
    );
  }
  return { dir, name: manifest.name };
}

function sleepMs(ms) {
  if (ms > 0) Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms);
}

export function publishPackages({
  version,
  distDir,
  exec = registryExec,
  dryRun = false,
  log = console.error,
  // Registry read replicas can lag right after a publish; re-check with
  // patience before declaring a partial state.
  verifyAttempts = 5,
  verifyDelayMs = 10_000,
  sleep = sleepMs,
}) {
  const channel = channelFor(version);

  // Platforms first, launcher strictly last.
  const packages = [
    ...targets.map((t) => loadPackage(distDir, t.npmName.split("/")[1], version)),
    loadPackage(distDir, "gskill", version),
  ];

  if (channel === "latest") {
    const current = exec.latestOf(LAUNCHER_NAME);
    if (current && compareVersions(current, version) > 0) {
      throw new Error(
        `publishing ${version} with --tag latest would downgrade latest from ${current} — operator intervention required`,
      );
    }
  }

  const outcomes = [];
  for (const pkg of packages) {
    if (exec.exists(pkg.name, version)) {
      log(`SKIP ${pkg.name}@${version} (already published)`);
      outcomes.push({ name: pkg.name, action: "skipped" });
    } else if (dryRun) {
      log(`PLAN ${pkg.name}@${version} --tag ${channel}`);
      outcomes.push({ name: pkg.name, action: "would-publish" });
    } else {
      log(`PUBLISH ${pkg.name}@${version} --tag ${channel}`);
      exec.publish(pkg.dir, pkg.name, channel);
      outcomes.push({ name: pkg.name, action: "published" });
    }
  }

  if (!dryRun) {
    let missing = packages;
    for (let attempt = 1; attempt <= verifyAttempts; attempt++) {
      missing = missing.filter((pkg) => !exec.exists(pkg.name, version));
      if (missing.length === 0) break;
      if (attempt < verifyAttempts) {
        log(`WAIT ${missing.length} package(s) not yet visible at ${version} (attempt ${attempt}/${verifyAttempts})`);
        sleep(verifyDelayMs);
      }
    }
    if (missing.length > 0) {
      throw new Error(
        `partial registry state: not available at ${version}: ${missing.map((p) => p.name).join(", ")} — operator intervention required`,
      );
    }
  }

  return { channel, outcomes };
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? "").href) {
  try {
    const { values } = parseArgs({
      options: {
        version: { type: "string" },
        dist: { type: "string" },
        "dry-run": { type: "boolean", default: false },
      },
    });
    if (!values.version || !values.dist) {
      throw new Error("usage: publish-packages.mjs --version X.Y.Z --dist <dir> [--dry-run]");
    }
    const result = publishPackages({
      version: values.version,
      distDir: values.dist,
      dryRun: values["dry-run"],
    });
    console.log(
      `${values["dry-run"] ? "planned" : "published"} ${result.outcomes.length} packages on tag ${result.channel}`,
    );
  } catch (err) {
    console.error(`publish-packages: ${err.message}`);
    process.exit(1);
  }
}
