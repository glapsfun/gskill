// Single source of truth for the Go target ↔ npm package mapping. The archive
// name template is a hard contract with .goreleaser.yaml and scripts/install.sh.
export const targets = [
  { goOs: "darwin", goArch: "amd64", nodePlatform: "darwin", nodeArch: "x64", npmName: "@glapsfun/gskill-darwin-x64" },
  { goOs: "darwin", goArch: "arm64", nodePlatform: "darwin", nodeArch: "arm64", npmName: "@glapsfun/gskill-darwin-arm64" },
  { goOs: "linux", goArch: "amd64", nodePlatform: "linux", nodeArch: "x64", npmName: "@glapsfun/gskill-linux-x64" },
  { goOs: "linux", goArch: "arm64", nodePlatform: "linux", nodeArch: "arm64", npmName: "@glapsfun/gskill-linux-arm64" },
];

export const supportedMatrix = targets
  .map((t) => `${t.nodePlatform}/${t.nodeArch}`)
  .join(", ");

export function lookup(nodePlatform, nodeArch) {
  return targets.find(
    (t) => t.nodePlatform === nodePlatform && t.nodeArch === nodeArch,
  );
}

export function archiveName(target, version) {
  return `gskill_${version}_${target.goOs}_${target.goArch}.tar.gz`;
}

// Single source of truth for version/tag shape — the workflow and the package
// builder both validate through these.
export const versionPattern = /^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/;

export function versionFromTag(tag) {
  if (typeof tag !== "string" || !tag.startsWith("v") || !versionPattern.test(tag.slice(1))) {
    throw new Error(
      `invalid release tag ${JSON.stringify(tag)} — expected vX.Y.Z or vX.Y.Z-<prerelease>`,
    );
  }
  return tag.slice(1);
}
