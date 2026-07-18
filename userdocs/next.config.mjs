import { createMDX } from 'fumadocs-mdx/next';

const withMDX = createMDX();

/** @type {import('next').NextConfig} */
const config = {
  reactStrictMode: true,
  // Static export: the site ships as plain files on a Cloudflare assets
  // Worker (same pattern as landing/ and frontend/). Search runs on a
  // build-time index (see app/api/search), and there is no middleware —
  // proxy.ts content negotiation is intentionally absent.
  output: 'export',
};

export default withMDX(config);
