// notifbuddy brand mark + wordmark, ported from the app's logo.svelte: a solid
// "ink" disc with a clay "signal dot" notched into the top-right. Brand-kit
// colors (not theme tokens) so it reads on any surface; the disc flips
// ink/paper between light and dark, and the dot's ring matches the background.

const SIZE = 22;
const DOT = Math.round(SIZE * 0.34);
const RING = Math.max(2, Math.round(SIZE * 0.07));

export function Logo() {
  return (
    <span className="inline-flex items-center gap-2" aria-label="notifbuddy">
      <span
        className="nb-mark relative flex-none"
        style={{ width: SIZE, height: SIZE }}
      >
        <span className="nb-disc absolute inset-0 rounded-full" />
        <span
          className="nb-dot absolute rounded-full"
          style={{
            width: DOT,
            height: DOT,
            top: -1,
            right: -1,
            border: `${RING}px solid var(--color-fd-background)`,
          }}
        />
      </span>
      <span className="font-semibold tracking-tight">notifbuddy</span>
    </span>
  );
}
