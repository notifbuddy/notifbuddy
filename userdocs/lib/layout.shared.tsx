import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';
import { gitConfig } from './shared';
import { Logo } from '@/components/logo';
import { LayoutDashboard } from 'lucide-react';

export function baseOptions(): BaseLayoutProps {
  return {
    nav: {
      title: <Logo />,
    },
    // Icon links render in the sidebar's footer row beside the GitHub icon
    // and theme toggle — fumadocs' own placement, no custom chrome.
    links: [
      {
        type: 'icon',
        icon: <LayoutDashboard />,
        text: 'Dashboard',
        label: 'Open the notifbuddy dashboard',
        url: 'https://dashboard.notifbuddy.com',
        external: true,
      },
    ],
    githubUrl: `https://github.com/${gitConfig.user}/${gitConfig.repo}`,
  };
}
