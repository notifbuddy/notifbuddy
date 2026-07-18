import { defineConfig, defineDocs } from 'fumadocs-mdx/config';
import { metaSchema, pageSchema } from 'fumadocs-core/source/schema';
import { z } from 'zod';

// You can customize Zod schemas for frontmatter and `meta.json` here
// see https://fumadocs.dev/docs/mdx/collections
export const docs = defineDocs({
  dir: 'content/docs',
  docs: {
    schema: pageSchema,
    postprocess: {
      includeProcessedMarkdown: true,
    },
  },
  meta: {
    schema: metaSchema,
  },
});

// Changelog entries: one frontmatter-only file per shipped change. Entries are
// short lines on the /changelog stream (description, two lines max) — no
// per-entry pages. A larger feature links out to a docs page via `link`.
export const changelog = defineDocs({
  dir: 'content/changelog',
  docs: {
    schema: pageSchema.extend({
      date: z.iso.date(),
      tags: z.array(z.string()).default([]),
      link: z.string().optional(),
    }),
  },
  meta: {
    schema: metaSchema,
  },
});

export default defineConfig({
  mdxOptions: {
    // MDX options
  },
});
