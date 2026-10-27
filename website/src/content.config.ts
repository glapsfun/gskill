// The docs collection is the tree `go run ./cmd/gen-site` writes from ../docs
// into Starlight's default src/content/docs, so the stock loader is enough.
import { defineCollection } from 'astro:content';
import { docsLoader } from '@astrojs/starlight/loaders';
import { docsSchema } from '@astrojs/starlight/schema';

export const collections = {
  docs: defineCollection({ loader: docsLoader(), schema: docsSchema() }),
};
