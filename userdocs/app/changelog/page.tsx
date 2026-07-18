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

const monthFormat = new Intl.DateTimeFormat('en', {
  year: 'numeric',
  month: 'long',
  timeZone: 'UTC',
});

export default function ChangelogIndex() {
  // Entries grouped by month (newest month first; entries inside stay
  // newest-first too, since changelogEntries() is already date-sorted).
  const months = new Map<string, ReturnType<typeof changelogEntries>>();
  for (const entry of changelogEntries()) {
    const key = entry.data.date.slice(0, 7); // YYYY-MM
    const group = months.get(key) ?? [];
    group.push(entry);
    months.set(key, group);
  }

  return (
    <main className="mx-auto w-full max-w-3xl flex-1 px-4 py-12">
      <h1 className="text-3xl font-semibold tracking-tight">Changelog</h1>
      <p className="text-fd-muted-foreground mt-2">
        Everything shipped in {appName}, newest first.{' '}
        <a href={`${changelogRoute}/feed.xml`} className="underline underline-offset-4">
          Atom feed
        </a>
      </p>
      <div className="mt-10 flex flex-col gap-12">
        {[...months.entries()].map(([month, entries]) => (
          <section key={month}>
            <h2 className="text-fd-muted-foreground border-b pb-2 text-sm font-medium tracking-wide uppercase">
              {monthFormat.format(new Date(`${month}-01`))}
            </h2>
            <div className="mt-6 flex flex-col gap-8">
              {entries.map((entry) => (
                <article key={entry.url} className="flex flex-col gap-1 sm:flex-row sm:gap-6">
                  <time
                    dateTime={entry.data.date}
                    className="text-fd-muted-foreground w-28 shrink-0 text-sm sm:pt-0.5"
                  >
                    {dateFormat.format(new Date(entry.data.date))}
                  </time>
                  <div>
                    <h3 className="text-lg font-semibold">
                      <Link href={entry.url} className="hover:underline underline-offset-4">
                        {entry.data.title}
                      </Link>
                    </h3>
                    {entry.data.description ? (
                      <p className="text-fd-muted-foreground mt-1.5">{entry.data.description}</p>
                    ) : null}
                    {entry.data.tags.length > 0 ? (
                      <div className="mt-2.5 flex flex-wrap gap-2">
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
                  </div>
                </article>
              ))}
            </div>
          </section>
        ))}
      </div>
    </main>
  );
}
