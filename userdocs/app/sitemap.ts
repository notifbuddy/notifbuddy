import type { MetadataRoute } from 'next';
import { source } from '@/lib/source';
import { changelogEntries } from '@/lib/changelog';
import { changelogRoute, siteUrl } from '@/lib/shared';

export const dynamic = 'force-static';

export default function sitemap(): MetadataRoute.Sitemap {
  const docs = source.getPages().map((page) => ({
    url: `${siteUrl}${page.url}`,
  }));
  const latest = changelogEntries()[0];

  return [
    { url: `${siteUrl}${changelogRoute}`, lastModified: latest?.data.date },
    ...docs,
  ];
}
