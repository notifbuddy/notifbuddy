import { source } from '@/lib/source';
import { DocsLayout } from 'fumadocs-ui/layouts/docs';
import { baseOptions } from '@/lib/layout.shared';
import { changelogRoute } from '@/lib/shared';

export default function Layout({ children }: LayoutProps<'/'>) {
  const base = source.getPageTree();
  // The changelog lives outside the docs content tree; append it to the
  // sidebar by hand so it's reachable from the left nav.
  const tree = {
    ...base,
    children: [
      ...base.children,
      { type: 'separator' as const, name: 'Product' },
      { type: 'page' as const, name: 'Changelog', url: changelogRoute },
    ],
  };

  return (
    <DocsLayout tree={tree} {...baseOptions()}>
      {children}
    </DocsLayout>
  );
}
