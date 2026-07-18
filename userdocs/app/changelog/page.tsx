import Link from 'next/link';
import type { Metadata } from 'next';
import { changelogEntries } from '@/lib/changelog';
import { appName, changelogRoute, siteUrl } from '@/lib/shared';

export const metadata: Metadata = {
  title: 'Changelog',
  description:
    'Everything shipped in notifbuddy — new syncing capabilities, integrations, and fixes, in order.',
  alternates: {
    canonical: `${siteUrl}${changelogRoute}`,
    types: { 'application/atom+xml': `${changelogRoute}/feed.xml` },
  },
};

const dateFormat = new Intl.DateTimeFormat('en', {
  year: 'numeric',
  month: 'long',
  day: 'numeric',
  timeZone: 'UTC',
});

export default function ChangelogIndex() {
  const entries = changelogEntries();

  return (
    <main className="mx-auto w-full max-w-3xl flex-1 px-4 py-12">
      <h1 className="text-3xl font-semibold tracking-tight">Changelog</h1>
      <p className="text-fd-muted-foreground mt-2">
        Everything shipped in {appName}, newest first.{' '}
        <a href={`${changelogRoute}/feed.xml`} className="underline underline-offset-4">
          Atom feed
        </a>
      </p>
      <div className="mt-10 flex flex-col gap-10">
        {entries.map((entry) => (
          <article key={entry.url}>
            <time
              dateTime={entry.data.date}
              className="text-fd-muted-foreground text-sm"
            >
              {dateFormat.format(new Date(entry.data.date))}
            </time>
            <h2 className="mt-1 text-xl font-semibold">
              <Link href={entry.url} className="hover:underline underline-offset-4">
                {entry.data.title}
              </Link>
            </h2>
            {entry.data.description ? (
              <p className="text-fd-muted-foreground mt-2">{entry.data.description}</p>
            ) : null}
            {entry.data.tags.length > 0 ? (
              <div className="mt-3 flex flex-wrap gap-2">
                {entry.data.tags.map((tag) => (
                  <span
                    key={tag}
                    className="bg-fd-muted text-fd-muted-foreground rounded-full px-2.5 py-0.5 text-xs"
                  >
                    {tag}
                  </span>
                ))}
              </div>
            ) : null}
          </article>
        ))}
      </div>
    </main>
  );
}
