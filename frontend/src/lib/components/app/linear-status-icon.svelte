<script lang="ts">
	// Linear-style workflow status glyph, drawn from a state's `type` + `color`.
	// Linear doesn't expose an icon image for statuses (custom or default) — its own
	// UI renders these category glyphs client-side, tinted by the state color, and we
	// do the same. The `type` is one of Linear's seven categories:
	//   triage     → filled disc with an exclamation
	//   backlog    → dashed open ring
	//   unstarted  → solid open ring
	//   started    → open ring with a partial pie fill (in progress)
	//   completed  → filled disc with a check
	//   canceled   → filled disc with an x
	//   duplicate  → filled disc with an x (same treatment as canceled)
	// Unknown/missing types fall back to a plain open ring.
	let {
		type = 'unstarted',
		color = 'currentColor',
		size = 14,
		class: className
	}: { type?: string; color?: string; size?: number; class?: string } = $props();

	// viewBox is 14×14; center (7,7), outer radius 6 leaves room for a 2px stroke.
	const c = 7;
	const r = 6;
	// A ~35% progress wedge from 12 o'clock, for the "started" glyph.
	const pie = (() => {
		const frac = 0.35;
		const a = 2 * Math.PI * frac - Math.PI / 2; // start at top (−90°)
		const inner = 3.5;
		return `M ${c} ${c} L ${c} ${c - inner} A ${inner} ${inner} 0 ${frac > 0.5 ? 1 : 0} 1 ${
			c + inner * Math.cos(a)
		} ${c + inner * Math.sin(a)} Z`;
	})();
</script>

<svg
	class={className}
	width={size}
	height={size}
	viewBox="0 0 14 14"
	fill="none"
	aria-hidden="true"
	style="color: {color}"
>
	{#if type === 'completed'}
		<circle cx={c} cy={c} {r} fill="currentColor" />
		<path
			d="M4.2 7.1 6.1 9l3.6-3.8"
			stroke="var(--background)"
			stroke-width="1.4"
			stroke-linecap="round"
			stroke-linejoin="round"
			fill="none"
		/>
	{:else if type === 'canceled' || type === 'duplicate'}
		<circle cx={c} cy={c} {r} fill="currentColor" />
		<path
			d="M5 5l4 4M9 5l-4 4"
			stroke="var(--background)"
			stroke-width="1.4"
			stroke-linecap="round"
			fill="none"
		/>
	{:else if type === 'triage'}
		<circle cx={c} cy={c} {r} fill="currentColor" />
		<path d="M7 3.6v3.6" stroke="var(--background)" stroke-width="1.4" stroke-linecap="round" />
		<circle cx={c} cy="10" r="0.85" fill="var(--background)" />
	{:else if type === 'started'}
		<circle cx={c} cy={c} {r} stroke="currentColor" stroke-width="2" fill="none" />
		<path d={pie} fill="currentColor" />
	{:else if type === 'backlog'}
		<circle
			cx={c}
			cy={c}
			{r}
			stroke="currentColor"
			stroke-width="2"
			stroke-dasharray="2.5 2.2"
			fill="none"
		/>
	{:else}
		<!-- unstarted / unknown: plain open ring -->
		<circle cx={c} cy={c} {r} stroke="currentColor" stroke-width="2" fill="none" />
	{/if}
</svg>
