<script lang="ts">
	// The branded error page — Quiet "signal lost" board for apps without
	// shadcn-svelte (landing). Dashboard kit/API errors use SignalErrorBoard
	// (Empty + Button + Badge) instead. Keep the layout and copy in sync.
	import Logo from './logo.svelte';

	let {
		status = 404,
		privacyHref = '/privacy',
		detail,
		homeHref = '/',
		ctaLabel = 'Back to the signal',
		title,
		code,
		supportLabel = 'reach out to support'
	}: {
		status?: number;
		privacyHref?: string;
		detail?: string;
		homeHref?: string;
		ctaLabel?: string;
		title?: string;
		code?: string | null;
		supportLabel?: string;
	} = $props();

	const notFound = $derived(status === 404);
	const resolvedTitle = $derived(
		title ?? (notFound ? 'Unable to find this page' : 'Unable to load this page')
	);
	const resolvedDetail = $derived(
		detail ??
			(notFound
				? "This page isn't syncing here — it may have moved, or it never existed. Nothing gets lost for long, though."
				: 'An unexpected error interrupted the signal. Give it a moment and try again.')
	);
</script>

<div class="landing flex min-h-svh flex-col">
	<header class="chrome-top">
		<div class="chrome-top-inner">
			<a href="/" aria-label="notifbuddy home"><Logo size={30} /></a>
		</div>
	</header>

	<main
		class="mx-auto flex w-full max-w-xl flex-1 flex-col items-center justify-center gap-4 px-4 py-10 text-center sm:px-6"
	>
		<svg class="lost-signal mb-2 w-full max-w-sm" viewBox="0 0 420 130" fill="none" aria-hidden="true">
			<circle cx="24" cy="35" r="5" class="node" />
			<circle cx="24" cy="65" r="5" class="node" />
			<circle cx="24" cy="95" r="5" class="node" />
			<path class="path" d="M29 35 C 90 35, 120 62, 170 65" />
			<path class="path" d="M29 65 L 170 65" />
			<path class="path" d="M29 95 C 90 95, 120 68, 170 65" />
			<path class="path" d="M170 65 L 264 65" />
			<path class="path broken" d="M272 65 L 344 65" />
			<circle cx="372" cy="65" r="17" class="ring" />
			<circle cx="352" cy="103" r="7" class="dot" />
		</svg>

		<div class="flex flex-wrap items-center justify-center gap-2">
			<span
				class="border-border bg-input/20 text-foreground inline-flex h-5 items-center rounded-full border px-2 text-[0.625rem] font-medium"
			>
				http {status}
			</span>
			{#if code}
				<span
					class="bg-secondary text-secondary-foreground inline-flex h-5 items-center rounded-full px-2 text-[0.625rem] font-medium"
				>
					{code}
				</span>
			{/if}
		</div>

		<h1 class="text-balance text-2xl font-medium tracking-tight sm:text-3xl">
			{resolvedTitle}<span class="text-primary">.</span>
		</h1>

		<p class="text-muted-foreground max-w-md text-sm/relaxed text-pretty">
			{resolvedDetail}
		</p>

		<div class="flex flex-col items-center gap-2 sm:flex-row">
			<a
				href={homeHref}
				class="bg-primary text-primary-foreground hover:bg-primary/80 inline-flex h-8 items-center justify-center rounded-md px-2.5 text-xs/relaxed font-medium transition-colors"
			>
				{ctaLabel}
			</a>
			<a
				href="mailto:support@notifbuddy.com"
				class="text-primary inline-flex h-6 items-center px-2 text-xs/relaxed font-medium underline-offset-4 hover:underline"
			>
				{supportLabel}
			</a>
		</div>
	</main>

	<footer class="chrome-bottom mx-auto flex w-full max-w-6xl items-center justify-between px-4 py-6 sm:px-6">
		<p class="text-muted-foreground/70 font-mono text-[11px] tracking-[0.12em]">
			© 2026 notifbuddy — all the noise, one signal
		</p>
		<a
			class="text-muted-foreground/70 hover:text-muted-foreground text-xs/relaxed font-medium underline-offset-4 hover:underline"
			href={privacyHref}>privacy</a
		>
	</footer>
</div>

<style>
	.landing {
		background:
			radial-gradient(
				110% 70% at 70% 0%,
				color-mix(in oklab, var(--primary) 7%, transparent),
				transparent 60%
			),
			var(--background);
	}

	.lost-signal .node {
		fill: color-mix(in oklab, var(--foreground) 22%, transparent);
	}
	.lost-signal .path {
		stroke: color-mix(in oklab, var(--foreground) 18%, transparent);
		stroke-width: 1.5;
	}
	.lost-signal .broken {
		stroke-dasharray: 5 9;
		stroke-linecap: round;
		opacity: 0.7;
	}
	.lost-signal .ring {
		stroke: color-mix(in oklab, var(--foreground) 30%, transparent);
		stroke-width: 1.5;
		stroke-dasharray: 4 6;
		stroke-linecap: round;
	}
	.lost-signal .dot {
		fill: var(--primary);
		animation: pip-breathe 3.5s ease-in-out infinite;
	}
	@keyframes pip-breathe {
		0%,
		100% {
			opacity: 1;
		}
		50% {
			opacity: 0.35;
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.lost-signal .dot {
			animation: none;
		}
	}
</style>
