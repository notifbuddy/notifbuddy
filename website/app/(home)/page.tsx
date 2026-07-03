import Link from 'next/link';

export default function HomePage() {
  return (
    <main className="flex flex-1 flex-col items-center justify-center gap-6 px-4 text-center">
      <h1 className="text-4xl font-bold tracking-tight">notifbuddy</h1>
      <p className="max-w-md text-fd-muted-foreground">
        Route your Linear and GitHub notifications into one calm Slack feed.
      </p>
      <Link
        href="/docs"
        className="rounded-[var(--radius)] bg-fd-primary px-5 py-2.5 font-medium text-fd-primary-foreground transition-opacity hover:opacity-90"
      >
        Read the docs
      </Link>
    </main>
  );
}
