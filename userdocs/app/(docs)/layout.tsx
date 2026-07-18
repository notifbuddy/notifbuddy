import { source } from '@/lib/source';
import { DocsLayout } from 'fumadocs-ui/layouts/docs';
import { baseOptions } from '@/lib/layout.shared';

export default function Layout({ children }: LayoutProps<'/'>) {
  return (
    <DocsLayout tree={source.getPageTree()} {...baseOptions()}>
      {/* display:contents keeps the layout untouched while giving the page a
          main landmark (fumadocs renders an article, not a main). */}
      <main style={{ display: 'contents' }}>{children}</main>
    </DocsLayout>
  );
}
