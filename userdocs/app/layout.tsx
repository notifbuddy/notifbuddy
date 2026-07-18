import { RootProvider } from 'fumadocs-ui/provider/next';
import './global.css';
import { Outfit, JetBrains_Mono } from 'next/font/google';
import type { Metadata } from 'next';
import { appName, siteUrl } from '@/lib/shared';

// Match the app's typography: Outfit (sans) + JetBrains Mono (code).
const sans = Outfit({ subsets: ['latin'], variable: '--font-sans' });
const mono = JetBrains_Mono({ subsets: ['latin'], variable: '--font-mono' });

export const metadata: Metadata = {
  metadataBase: new URL(siteUrl),
  title: {
    template: `%s | ${appName} docs`,
    default: `${appName} docs`,
  },
  description:
    'Documentation and changelog for notifbuddy — two-way sync between Linear and Slack.',
};

export default function Layout({ children }: LayoutProps<'/'>) {
  return (
    <html
      lang="en"
      className={`${sans.variable} ${mono.variable}`}
      suppressHydrationWarning
    >
      <body className="flex flex-col min-h-screen">
        {/* Static export: search runs on the build-time index in the browser. */}
        <RootProvider search={{ options: { type: 'static' } }}>
          {children}
        </RootProvider>
      </body>
    </html>
  );
}
