import { redirect } from 'next/navigation';
import { docsRoute } from '@/lib/shared';

// No standalone landing page — the docs are the front door.
export default function Page() {
  redirect(docsRoute);
}
