<script lang="ts">
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
