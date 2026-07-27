import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { channelFor, compareVersions, publishPackages } from "../scripts/publish-packages.mjs";
import { targets } from "../lib/platforms.js";

const LAUNCHER = "@glapsfun/gskill";

function makeDist(version) {
  const dist = fs.mkdtempSync(path.join(os.tmpdir(), "gskill-publish-"));
  const write = (dirName, name) => {
    const dir = path.join(dist, dirName);
    fs.mkdirSync(dir, { recursive: true });
    fs.writeFileSync(path.join(dir, "package.json"), JSON.stringify({ name, version }));
  };
  for (const t of targets) write(t.npmName.split("/")[1], t.npmName);
  write("gskill", LAUNCHER);
  return dist;
}

function fakeExec({ existing = [], latest = null, failPublishFor = [] } = {}) {
  const calls = { publish: [], exists: [], latestOf: [] };
  const published = new Set(existing);
  return {
    calls,
    exec: {
      exists(name, version) {
        calls.exists.push(`${name}@${version}`);
        return published.has(name);
      },
      latestOf(name) {
        calls.latestOf.push(name);
        return latest;
      },
      publish(dir, name, tag) {
        if (failPublishFor.includes(name)) throw new Error(`registry rejected ${name}`);
        calls.publish.push({ name, tag });
        published.add(name);
      },
    },
  };
}

test("channelFor maps stable to latest and prerelease to next", () => {
  assert.equal(channelFor("1.2.3"), "latest");
  assert.equal(channelFor("0.6.0-rc.1"), "next");
  assert.equal(channelFor("2.0.0-beta.2"), "next");
});

test("compareVersions follows semver precedence including prereleases", () => {
  assert.equal(compareVersions("1.2.3", "1.2.3"), 0);
  assert.equal(compareVersions("1.2.3", "1.10.0"), -1);
  assert.equal(compareVersions("1.2.3-rc.1", "1.2.3"), -1);
  assert.equal(compareVersions("1.2.3", "1.2.3-rc.1"), 1);
  assert.equal(compareVersions("1.2.3-rc.2", "1.2.3-rc.10"), -1);
  assert.equal(compareVersions("1.2.3-rc.10", "1.2.3-rc.2"), 1);
  assert.equal(compareVersions("1.2.3-alpha", "1.2.3-alpha.1"), -1);
  assert.equal(compareVersions("1.2.3-1", "1.2.3-alpha"), -1);
});

test("platforms publish first, launcher strictly last, on the right tag", () => {
  const dist = makeDist("1.2.3");
  const { exec, calls } = fakeExec();
  const result = publishPackages({ version: "1.2.3", distDir: dist, exec });
  assert.equal(calls.publish.length, 5);
  assert.equal(calls.publish[4].name, LAUNCHER);
  assert.deepEqual(
    calls.publish.slice(0, 4).map((c) => c.name).sort(),
    targets.map((t) => t.npmName).sort(),
  );
  for (const c of calls.publish) assert.equal(c.tag, "latest");
  assert.equal(result.channel, "latest");
});

test("prerelease publishes on the next tag", () => {
  const dist = makeDist("1.3.0-rc.1");
  const { exec, calls } = fakeExec();
  publishPackages({ version: "1.3.0-rc.1", distDir: dist, exec });
  for (const c of calls.publish) assert.equal(c.tag, "next");
});

test("already-published exact versions are validated and skipped, not overwritten", () => {
  const dist = makeDist("1.2.3");
  const preExisting = [targets[0].npmName, targets[1].npmName];
  const { exec, calls } = fakeExec({ existing: preExisting });
  const result = publishPackages({ version: "1.2.3", distDir: dist, exec });
  assert.equal(calls.publish.length, 3);
  for (const name of preExisting) {
    assert.ok(!calls.publish.some((c) => c.name === name), `${name} must not republish`);
  }
  const skipped = result.outcomes.filter((o) => o.action === "skipped").map((o) => o.name);
  assert.deepEqual(skipped.sort(), preExisting.sort());
});

test("fully-published release converges to all-skip", () => {
  const dist = makeDist("1.2.3");
  const all = [...targets.map((t) => t.npmName), LAUNCHER];
  const { exec, calls } = fakeExec({ existing: all });
  const result = publishPackages({ version: "1.2.3", distDir: dist, exec });
  assert.equal(calls.publish.length, 0);
  assert.ok(result.outcomes.every((o) => o.action === "skipped"));
});

test("a stable retry that would downgrade latest fails before any publish", () => {
  const dist = makeDist("1.2.3");
  const { exec, calls } = fakeExec({ latest: "1.4.0" });
  assert.throws(
    () => publishPackages({ version: "1.2.3", distDir: dist, exec }),
    /downgrade.*latest|latest.*downgrade/i,
  );
  assert.equal(calls.publish.length, 0);
});

test("prerelease ignores the latest-downgrade guard", () => {
  const dist = makeDist("1.2.3-rc.9");
  const { exec, calls } = fakeExec({ latest: "9.9.9" });
  publishPackages({ version: "1.2.3-rc.9", distDir: dist, exec });
  assert.equal(calls.publish.length, 5);
});

test("mid-run publish failure propagates for operator intervention", () => {
  const dist = makeDist("1.2.3");
  const { exec, calls } = fakeExec({ failPublishFor: [targets[2].npmName] });
  assert.throws(
    () => publishPackages({ version: "1.2.3", distDir: dist, exec }),
    /registry rejected/,
  );
  assert.ok(!calls.publish.some((c) => c.name === LAUNCHER), "launcher must not publish after a platform failure");
});

test("post-run verification retries, then fails on persistent partial state", () => {
  const dist = makeDist("1.2.3");
  const { exec } = fakeExec();
  exec.publish = () => {}; // registry silently drops the publish
  const sleeps = [];
  assert.throws(
    () =>
      publishPackages({
        version: "1.2.3",
        distDir: dist,
        exec,
        verifyAttempts: 3,
        verifyDelayMs: 5,
        sleep: (ms) => sleeps.push(ms),
      }),
    /partial|not available/i,
  );
  assert.deepEqual(sleeps, [5, 5], "must wait between verification attempts");
});

test("post-run verification tolerates registry replication lag", () => {
  const dist = makeDist("1.2.3");
  const { exec } = fakeExec();
  const realExists = exec.exists;
  let checks = 0;
  exec.exists = (name, version) => {
    checks++;
    // First verification round: freshly published versions not visible yet.
    if (checks > 5 && checks <= 10) return false;
    return realExists(name, version);
  };
  const result = publishPackages({
    version: "1.2.3",
    distDir: dist,
    exec,
    verifyAttempts: 3,
    verifyDelayMs: 0,
    sleep: () => {},
  });
  assert.equal(result.outcomes.filter((o) => o.action === "published").length, 5);
});

test("dist manifests at a different version are refused", () => {
  const dist = makeDist("1.2.2");
  const { exec, calls } = fakeExec();
  assert.throws(
    () => publishPackages({ version: "1.2.3", distDir: dist, exec }),
    /version mismatch/i,
  );
  assert.equal(calls.publish.length, 0);
});

test("dry-run plans everything and publishes nothing", () => {
  const dist = makeDist("1.2.3");
  const { exec, calls } = fakeExec({ existing: [targets[0].npmName] });
  const result = publishPackages({ version: "1.2.3", distDir: dist, exec, dryRun: true });
  assert.equal(calls.publish.length, 0);
  const actions = Object.fromEntries(result.outcomes.map((o) => [o.name, o.action]));
  assert.equal(actions[targets[0].npmName], "skipped");
  assert.equal(actions[LAUNCHER], "would-publish");
});
