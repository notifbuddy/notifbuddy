<script lang="ts">
	// Live status pill from the Better Stack status page, embedded as an iframe.
	//
	// No click overlay: the markup Better Stack serves is already an
	// <a target="_blank"> wrapping the whole pill, so it navigates on its own.
	// An overlay would only steal its hover state.
	//
	// The theme param is not optional. Left off, the badge reads the OS scheme
	// itself and would ignore our own light/dark toggle — a dark pill on a light
	// page for anyone who overrode their system setting. Callers pass the
	// resolved mode (mode-watcher's `mode.current`); this lives outside both
	// apps' package roots, so it can't import mode-watcher itself.
	//
	// Until the badge is live — prerender, lazy-load, or the iframe never
	// arriving at all — a plain link stands in. Both sit in one grid cell, so
	// the swap costs no layout shift.
	//
	// `width` is how much room the badge claims, which is not the same as how
	// much it paints. Better Stack renders the pill left-aligned at x=0 inside a
	// 250px frame — 250 fits their longest status string, so anything shorter
	// leaves transparent dead space on the right. Centering the full 250 puts the
	// pill visibly left of centre ("All services are online" is ~182px, so ~34px
	// off). Claiming only the ink width fixes that, and because the frame still
	// paints 250 and nothing clips it, a longer status during an incident spills
	// right over that transparent space rather than being truncated. Only narrow
	// `width` where there is nothing immediately to the right to collide with.
	const PAINT_WIDTH = 250;

	let {
		href = 'https://status.notifbuddy.com',
		theme = 'light',
		label = 'check service status',
		width = PAINT_WIDTH,
		linkClass = '',
		class: className = ''
	}: {
		href?: string;
		theme?: 'light' | 'dark';
		label?: string;
		width?: number;
		linkClass?: string;
		class?: string;
	} = $props();

	let live = $state(false);

	const src = $derived(`${href}/badge?theme=${theme}`);
</script>

<span
	class={['grid items-center', className].filter(Boolean).join(' ')}
	style="width:{width}px"
>
	<!-- The stand-in gets the claimed width, not the grid track — the track is
	     sized by the 250px frame, so centring in it would land the text where
	     the pill's dead space is. Pass text-center in linkClass to centre it. -->
	<a
		{href}
		target="_blank"
		rel="noopener"
		style="width:{width}px"
		class={['col-start-1 row-start-1 justify-self-start', linkClass]
			.filter(Boolean)
			.join(' ')}
		class:invisible={live}
		aria-hidden={live}
		tabindex={live ? -1 : undefined}
	>
		{label}
	</a>
	<iframe
		title="notifbuddy service status"
		{src}
		width={PAINT_WIDTH}
		height="30"
		loading="lazy"
		scrolling="no"
		style="color-scheme: normal"
		class="col-start-1 row-start-1 border-0"
		class:invisible={!live}
		onload={() => (live = true)}
	></iframe>
</span>
