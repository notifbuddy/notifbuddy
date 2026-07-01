<script lang="ts">
	import { cn } from '$lib/utils';

	// The notifbuddy brand mark — a solid "ink" disc with a clay "signal dot"
	// notched into the top-right, optionally followed by the lowercase wordmark.
	// From the NotifBuddy brand kit ("Quiet" direction): the dot is the brand —
	// keep it clay, round, and attached. Colors are the kit's own brand values
	// (not app theme tokens) so the logo reads consistently on any surface; only
	// the disc + the dot's ring flip between light/dark to sit on the background.
	let {
		size = 28,
		wordmark = true,
		class: className
	}: { size?: number; wordmark?: boolean; class?: string } = $props();

	// Proportions taken from the brand-kit lockup (dot ≈ 0.34× the disc, ring ≈
	// 0.07×), so the mark holds its look at any size.
	const dot = $derived(Math.round(size * 0.34));
	const ring = $derived(Math.max(2, Math.round(size * 0.07)));
</script>

<span
	class={cn('inline-flex items-center', wordmark ? 'gap-2.5' : '', className)}
	aria-label="notifbuddy"
>
	<!-- Mark. The disc flips ink/paper for contrast on the surface; the clay dot's
	     ring matches --background so it reads as attached-but-separate. -->
	<span class="nb-mark relative flex-none" style="width:{size}px;height:{size}px;">
		<span class="nb-disc absolute inset-0 rounded-full"></span>
		<span
			class="nb-dot absolute rounded-full"
			style="
				width:{dot}px;height:{dot}px;top:-1px;right:-1px;
				border:{ring}px solid var(--background);
			"
		></span>
	</span>

	{#if wordmark}
		<span
			class="font-semibold leading-none tracking-tight"
			style="font-size:{Math.round(size * 0.82)}px;"
		>
			notifbuddy
		</span>
	{/if}
</span>

<style>
	/* Brand-kit values. The disc is warm near-black on light and warm paper on
	   dark; clay brightens slightly in dark mode to hold against the deep canvas. */
	.nb-disc {
		background: #2a2925;
	}
	.nb-dot {
		background: oklch(0.64 0.058 45);
	}
	:global(.dark) .nb-disc {
		background: #f3f0e9;
	}
	:global(.dark) .nb-dot {
		background: oklch(0.7 0.06 48);
	}
</style>
