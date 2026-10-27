// @ts-check
// GSKILL documentation site. The pages under src/content/docs and the sidebar
// in src/generated are written by `go run ./cmd/gen-site` from ../docs; run it
// (or scripts/site.sh) before `astro dev` or `astro build`.
import { existsSync, readFileSync } from 'node:fs';
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightLinksValidator from 'starlight-links-validator';

const repo = 'https://github.com/glapsfun/gskill';

const sidebarPath = new URL('./src/generated/sidebar.json', import.meta.url);
if (!existsSync(sidebarPath)) {
  throw new Error('src/generated/sidebar.json is missing: run `go run ./cmd/gen-site` from the repository root first');
}
const sidebar = JSON.parse(readFileSync(sidebarPath, 'utf8'));

// site and base must match internal/docsite (Site, BasePath); test/docs checks it.
export default defineConfig({
  site: 'https://glapsfun.github.io',
  base: '/gskill',
  trailingSlash: 'always',
  outDir: './dist',
  integrations: [
    starlight({
      title: 'GSKILL',
      description: 'Reproducible package management for AI agent skills.',
      social: [{ icon: 'github', label: 'GitHub', href: repo }],
      favicon: '/favicon.svg',
      customCss: ['./src/styles/custom.css'],
      head: [
        // Without scripts, Starlight's tabs show only the first panel. Show
        // every install method instead, and drop the tab strip that cannot work.
        {
          tag: 'noscript',
          content:
            "<style>starlight-tabs [role='tabpanel'][hidden]{display:block}starlight-tabs [role='tablist']{display:none}</style>",
        },
      ],
      editLink: { baseUrl: `${repo}/edit/main/docs/` },
      sidebar,
      components: { SocialIcons: './src/components/SocialIcons.astro' },
      // Fails the build on a broken internal link or a missing heading anchor.
      plugins: [starlightLinksValidator()],
    }),
  ],
});
