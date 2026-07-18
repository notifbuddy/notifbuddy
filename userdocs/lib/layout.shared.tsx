import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';
import { gitConfig } from './shared';
import { Logo } from '@/components/logo';

export function baseOptions(): BaseLayoutProps {
  return {
    nav: {
      title: <Logo />,
    },
    links: [
      { text: 'Docs', url: '/', active: 'nested-url' },
      { text: 'Dashboard', url: 'https://dashboard.notifbuddy.com', external: true },
    ],
    githubUrl: `https://github.com/${gitConfig.user}/${gitConfig.repo}`,
  };
}
