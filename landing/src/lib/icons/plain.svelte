<script lang="ts">
	// Plain (plain.com) brand mark as a monochrome (currentColor) glyph — a
	// two-part "P" traced from their public avatar; Simple Icons doesn't carry
	// Plain, so we ship it locally like slack.svelte. Mirrors the
	// @icons-pack/svelte-simple-icons component API (size + ...rest).
	let { size = 24, ...rest }: { size?: number; [key: string]: unknown } = $props();
</script>

<svg
	xmlns="http://www.w3.org/2000/svg"
	width={size}
	height={size}
	viewBox="0 0 24 24"
	fill="currentColor"
	{...rest}
>
	<path d="M10.7 3.2h2.7a5.35 5.35 0 1 1 0 10.7H5.2z" />
	<path d="M16.3 15.1h-6.4l-4 5.7h7a4.5 4.5 0 0 0 3.4-5.7z" />
</svg>
