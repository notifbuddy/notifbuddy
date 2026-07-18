import type { MetadataRoute } from 'next';
import { source } from '@/lib/source';
import { changelogEntries } from '@/lib/changelog';
import { changelogRoute, siteUrl } from '@/lib/shared';

export const dynamic = 'force-static';

export default function sitemap(): MetadataRoute.Sitemap {
  const docs = source.getPages().map((page) => ({
    url: `${siteUrl}${page.url}`,
  }));
  const changelog = changelogEntries().map((entry) => ({
    url: `${siteUrl}${entry.url}`,
    lastModified: entry.data.date,
  }));

  return [{ url: `${siteUrl}${changelogRoute}` }, ...docs, ...changelog];
}
