import Link from 'next/link';
import { notFound } from 'next/navigation';
import type { Metadata } from 'next';
import { changelogEntries, changelogSource } from '@/lib/changelog';
import { getMDXComponents } from '@/components/mdx';
import { appName, changelogRoute, siteUrl } from '@/lib/shared';

const dateFormat = new Intl.DateTimeFormat('en', {
  year: 'numeric',
  month: 'long',
  day: 'numeric',
  timeZone: 'UTC',
});

export default async function ChangelogEntry(props: {
  params: Promise<{ slug: string }>;
}) {
  const { slug } = await props.params;
  const page = changelogSource.getPage([slug]);
  if (!page) notFound();

  const MDX = page.data.body;
  // TechArticle structured data: gives crawlers the publish date + headline
  // that a changelog entry is actually about.
  const jsonLd = {
    '@context': 'https://schema.org',
    '@type': 'TechArticle',
    headline: page.data.title,
    description: page.data.description,
    datePublished: page.data.date,
    url: `${siteUrl}${page.url}`,
    author: { '@type': 'Organization', name: appName, url: 'https://notifbuddy.com' },
  };

  return (
    <main className="mx-auto w-full max-w-3xl flex-1 px-4 py-12">
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd) }}
      />
      <Link
        href={changelogRoute}
        className="text-fd-muted-foreground text-sm hover:underline underline-offset-4"
      >
        ← Changelog
      </Link>
      <time
        dateTime={page.data.date}
        className="text-fd-muted-foreground mt-6 block text-sm"
      >
        {dateFormat.format(new Date(page.data.date))}
      </time>
      <h1 className="mt-1 text-3xl font-semibold tracking-tight">{page.data.title}</h1>
      <div className="prose mt-8">
        <MDX components={getMDXComponents()} />
      </div>
    </main>
  );
}

export function generateStaticParams() {
  return changelogEntries().map((entry) => ({ slug: entry.slugs[0] }));
}

export async function generateMetadata(props: {
  params: Promise<{ slug: string }>;
}): Promise<Metadata> {
  const { slug } = await props.params;
  const page = changelogSource.getPage([slug]);
  if (!page) notFound();

  return {
    title: page.data.title,
    description: page.data.description,
    alternates: { canonical: `${siteUrl}${page.url}` },
    openGraph: {
      title: page.data.title,
      description: page.data.description,
      type: 'article',
      publishedTime: page.data.date,
      url: `${siteUrl}${page.url}`,
    },
  };
}
