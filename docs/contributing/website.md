# Work on the documentation site

The site at https://glapsfun.github.io/gskill/ is built from the Markdown in `docs/`. You edit
`docs/`; the site is regenerated from it on every build, so there is no second copy to keep in
sync.

## How it fits together

| Path | Role |
| --- | --- |
| `docs/` | The documentation. The only place prose is edited. |
| `docs/nav.yaml` | Sidebar sections and their pages, plus pages kept off the site. |
| `cmd/gen-site`, `internal/docsite` | Turns `docs/` into the site's content: titles, descriptions, rewritten links, sidebar. |
| `website/` | The Astro + Starlight site. Its `src/content/docs/` and `src/generated/` are generated and gitignored. |
| `website/src/landing/` | The landing page (`index.mdx`) and its workflow example (`example.sh`). |
| `examples/skills/hello-gskill/` | The skill every runnable example installs. |

## Prerequisites

- Go, at the version in `go.mod`
- Node.js 24 (the site needs 22.23.4 or newer)
- Git

## Commands

Run these from the repository root. Each one regenerates the content from `docs/` first.

| Command | What it does |
| --- | --- |
| `scripts/site.sh dev` | Dev server with live reload at `http://localhost:4321/gskill/` |
| `scripts/site.sh build` | Production build into `website/dist`; fails on broken links or anchors |
| `scripts/site.sh preview` | Build, then serve `website/dist` at `http://localhost:4321/gskill/` |
| `scripts/site.sh check` | Everything the CI `site` job runs |
| `scripts/site.sh gen` | Only regenerate the content and the README install section |

`dev` picks up changes under `website/` immediately. After editing `docs/`, run
`scripts/site.sh gen` in another terminal to refresh the content.

## Add, move, or hide a page

1. Create or move the Markdown file under `docs/`. It needs a `# Title` and a first paragraph:
   the paragraph becomes the page's description in search results and link previews.
2. List it in a group in `docs/nav.yaml`, or add it to `exclude` with a reason to keep it off
   the site.
3. Link to other pages with ordinary relative links (`../how-to/work-offline.md`). They keep
   working on GitHub, and the generator rewrites them to site URLs. Links to files outside
   `docs/` go to GitHub.

`go test ./test/docs/...` and the generator both fail on a page that is neither listed nor
excluded, on a listed page that does not exist, and on a link whose target does not exist.

## Change the install commands

The install commands live in one place: the region between `<!-- install-methods:start -->` and
`<!-- install-methods:end -->` in [docs/installation.md](../installation.md). Each method is a
`###` heading, an optional one-paragraph note, and one or more `sh` code blocks that hold only
commands, with no comments or `$` prompts, because each block is copied as a unit.

After editing it, run `scripts/site.sh gen`. That copies the region into `README.md`; commit
both files. The CI `site` job fails if the README falls out of step.

## The landing page and its example

`website/src/landing/index.mdx` is the landing page. Its workflow block is
`website/src/landing/example.sh`, shown on the page exactly as written.

`scripts/check-landing-example.sh` runs that file line by line in a fresh Git project:

- `--local` uses gskill built from your checkout and serves the example skill from your working
  tree, offline. The CI `site` job runs this on every pull request.
- `--release` installs a published release (the one in `VERSION`, or the latest stable release)
  and fetches the example skill from GitHub. `.github/workflows/landing-example.yml` runs it after
  each release and after changes to the example on `main`, and reports when the site has moved
  ahead of what visitors can install.

## Generated reference pages

[Commands](../reference/commands.md) and [exit codes](../reference/exit-codes.md) are generated
from the CLI by `go run ./cmd/gen-reference`. Do not edit them by hand. On the site they carry a
note saying so instead of an edit link.

## Publishing

Merging to `main` publishes the site through `.github/workflows/pages.yml`, and so does every
release. You can also start it by hand from the Actions tab. See the
[CI/CD guide](../ci-cd.md#documentation-site) for the jobs and their permissions.

## One-time repository setup

A maintainer with admin rights does this once:

1. Settings, Pages, Build and deployment: set Source to **GitHub Actions**.
2. Settings, Environments, `github-pages`: limit deployment branches to `main`.
3. After the first successful deploy, set the repository website:
   `gh repo edit glapsfun/gskill --homepage https://glapsfun.github.io/gskill/`.
4. Settings, Rules: require the `site` and `verify` status checks on `main`, so a pull request
   that breaks the site cannot merge.
