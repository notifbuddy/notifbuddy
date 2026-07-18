import { changelog } from 'collections/server';
import { loader } from 'fumadocs-core/source';
import { changelogRoute } from './shared';

export const changelogSource = loader({
  baseUrl: changelogRoute,
  source: changelog.toFumadocsSource(),
});

export type ChangelogPage = (typeof changelogSource)['$inferPage'];

// Entries newest-first — the order both the index and the feed present.
export function changelogEntries(): ChangelogPage[] {
  return [...changelogSource.getPages()].sort((a, b) =>
    b.data.date.localeCompare(a.data.date),
  );
}
