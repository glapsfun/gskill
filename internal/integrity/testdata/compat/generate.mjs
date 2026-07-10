// Fixture-hash generator for the compat corpus.
//
// computeSkillFolderHash and collectFiles below are copied VERBATIM from the
// reference implementation: vercel-labs/skills src/local-lock.ts at commit
// 4ce6d48ac44c8b637db87b2102fea3baca719df1 (the `npx skills` CLI). Do not
// "improve" them — byte-for-byte parity with upstream is the whole point.
//
// Usage: node generate.mjs   (from this directory; rewrites expected/*.json)

import { readFile, readdir, writeFile, mkdir } from 'fs/promises';
import { join, relative } from 'path';
import { createHash } from 'crypto';

export async function computeSkillFolderHash(skillDir) {
  const files = [];
  await collectFiles(skillDir, skillDir, files);

  // Sort by relative path for deterministic hashing
  files.sort((a, b) => a.relativePath.localeCompare(b.relativePath));

  const hash = createHash('sha256');
  for (const file of files) {
    // Include the path in the hash so renames are detected
    hash.update(file.relativePath);
    hash.update(file.content);
  }

  return hash.digest('hex');
}

async function collectFiles(baseDir, currentDir, results) {
  const entries = await readdir(currentDir, { withFileTypes: true });

  await Promise.all(
    entries.map(async (entry) => {
      const fullPath = join(currentDir, entry.name);

      if (entry.isDirectory()) {
        // Skip .git and node_modules within skill dirs
        if (entry.name === '.git' || entry.name === 'node_modules') return;
        await collectFiles(baseDir, fullPath, results);
      } else if (entry.isFile()) {
        const content = await readFile(fullPath);
        const relativePath = relative(baseDir, fullPath).split('\\').join('/');
        results.push({ relativePath, content });
      }
    })
  );
}

const here = new URL('.', import.meta.url).pathname;
const fixturesDir = join(here, 'fixtures');
const expectedDir = join(here, 'expected');
await mkdir(expectedDir, { recursive: true });

for (const entry of await readdir(fixturesDir, { withFileTypes: true })) {
  if (!entry.isDirectory()) continue;
  const hash = await computeSkillFolderHash(join(fixturesDir, entry.name));
  const out = {
    computedHash: hash,
    recordedWith: 'vercel-labs/skills src/local-lock.ts @ 4ce6d48',
    node: process.version,
  };
  await writeFile(
    join(expectedDir, `${entry.name}.json`),
    JSON.stringify(out, null, 2) + '\n',
    'utf-8'
  );
  console.log(entry.name, hash);
}
