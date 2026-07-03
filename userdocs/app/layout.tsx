import { RootProvider } from 'fumadocs-ui/provider/next';
import './global.css';
import { Outfit, JetBrains_Mono } from 'next/font/google';

// Match the app's typography: Outfit (sans) + JetBrains Mono (code).
const sans = Outfit({ subsets: ['latin'], variable: '--font-sans' });
const mono = JetBrains_Mono({ subsets: ['latin'], variable: '--font-mono' });

export default function Layout({ children }: LayoutProps<'/'>) {
  return (
    <html
      lang="en"
      className={`${sans.variable} ${mono.variable}`}
      suppressHydrationWarning
    >
      <body className="flex flex-col min-h-screen">
        <RootProvider>{children}</RootProvider>
      </body>
    </html>
  );
}
