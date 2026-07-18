import { source } from '@/lib/source';
import { DocsLayout } from 'fumadocs-ui/layouts/docs';
import { baseOptions } from '@/lib/layout.shared';
import { gitConfig } from '@/lib/shared';

export default function Layout({ children }: LayoutProps<'/'>) {
  return (
    <DocsLayout
      tree={source.getPageTree()}
      {...baseOptions()}
      sidebar={{
        footer: (
          <div className="text-fd-muted-foreground flex flex-col gap-1.5 text-sm">
            <a
              href={`https://github.com/${gitConfig.user}/${gitConfig.repo}`}
              rel="noopener"
              className="hover:text-fd-foreground"
            >
              GitHub
            </a>
            <a
              href="https://dashboard.notifbuddy.com"
              rel="noopener"
              className="hover:text-fd-foreground"
            >
              Dashboard
            </a>
          </div>
        ),
      }}
    >
      {children}
    </DocsLayout>
  );
}
