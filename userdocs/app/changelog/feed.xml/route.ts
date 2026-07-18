import { changelogEntries } from '@/lib/changelog';
import { appName, changelogRoute, siteUrl } from '@/lib/shared';

export const revalidate = false;

const escapeXml = (s: string) =>
  s
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;');

// Atom feed of the changelog — feeds get crawled and syndicated, and give
// subscribers a zero-effort way to follow releases.
export function GET() {
  const entries = changelogEntries();
  const updated = entries[0] ? `${entries[0].data.date}T00:00:00Z` : new Date(0).toISOString();

  const items = entries
    .map((entry) => {
      const url = `${siteUrl}${entry.url}`;
      return `  <entry>
    <title>${escapeXml(entry.data.title)}</title>
    <link href="${url}"/>
    <id>${url}</id>
    <updated>${entry.data.date}T00:00:00Z</updated>
    <summary>${escapeXml(entry.data.description ?? '')}</summary>
  </entry>`;
    })
    .join('\n');

  const feed = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>${appName} changelog</title>
  <link href="${siteUrl}${changelogRoute}"/>
  <link rel="self" href="${siteUrl}${changelogRoute}/feed.xml"/>
  <id>${siteUrl}${changelogRoute}</id>
  <updated>${updated}</updated>
${items}
</feed>
`;

  return new Response(feed, {
    headers: { 'Content-Type': 'application/atom+xml; charset=utf-8' },
  });
}
