<script lang="ts">
	// The branded error page — the "signal lost" board, shared by both apps
	// via the `$shared` alias. On the landing site it's rendered by
	// routes/404/+page.svelte (prerendered to build/404.html, which Cloudflare
	// serves for unmatched paths) and routes/+error.svelte; in the dashboard
	// SPA by routes/+error.svelte. Theme tokens (--primary etc.) come from
	// whichever app renders it — both share the same QraftHive values, so the
	// page looks identical on either surface.
	import Logo from './logo.svelte';

	let {
		status = 404,
		privacyHref = '/privacy'
	}: { status?: number; privacyHref?: string } = $props();
	const notFound = $derived(status === 404);
</script>

<div class="landing flex min-h-svh flex-col">
	<header class="mx-auto flex w-full max-w-6xl items-center px-4 py-5 sm:px-6">
		<a href="/" aria-label="notifbuddy home"><Logo size={30} /></a>
	</header>

	<main
		class="mx-auto flex w-full max-w-xl flex-1 flex-col items-center justify-center gap-6 px-4 py-10 text-center sm:px-6"
	>
		<!-- The lost signal: source nodes feed a path that breaks up before it
		     reaches its destination ring; the brand's clay dot sits fallen just
		     beneath, still breathing. Decorative — the text carries the meaning. -->
		<svg class="lost-signal w-full max-w-sm" viewBox="0 0 420 130" fill="none" aria-hidden="true">
			<!-- source nodes (the tools) -->
			<circle cx="24" cy="35" r="5" class="node" />
			<circle cx="24" cy="65" r="5" class="node" />
			<circle cx="24" cy="95" r="5" class="node" />
			<!-- paths converging into one signal -->
			<path class="path" d="M29 35 C 90 35, 120 62, 170 65" />
			<path class="path" d="M29 65 L 170 65" />
			<path class="path" d="M29 95 C 90 95, 120 68, 170 65" />
			<!-- the one signal, holding steady… -->
			<path class="path" d="M170 65 L 264 65" />
			<!-- …then breaking up -->
			<path class="path broken" d="M272 65 L 344 65" />
			<!-- the destination that never received it -->
			<circle cx="372" cy="65" r="17" class="ring" />
			<!-- the fallen signal dot -->
			<circle cx="352" cy="103" r="7" class="dot" />
		</svg>

		<p class="eyebrow font-mono text-xs tracking-[0.18em] uppercase">
			<span class="eyebrow-pip" aria-hidden="true"></span>
			http {status} — {notFound ? 'signal lost' : 'signal interrupted'}
		</p>

		<h1 class="text-4xl leading-[1.05] font-semibold tracking-tight text-balance sm:text-5xl">
			{#if notFound}
				All the noise.<br />No signal<span class="text-primary">.</span>
			{:else}
				Something broke<br />the sync<span class="text-primary">.</span>
			{/if}
		</h1>

		<p class="text-muted-foreground max-w-md text-base leading-relaxed text-pretty">
			{#if notFound}
				This page isn't syncing here — it may have moved, or it never existed. Nothing gets lost
				for long, though.
			{:else}
				An unexpected error interrupted the signal. Give it a moment and try again.
			{/if}
		</p>

		<div class="flex flex-col items-center gap-3 sm:flex-row">
			<a
				href="/"
				class="bg-primary text-primary-foreground hover:bg-primary/90 inline-flex h-10 items-center justify-center rounded-md px-5 text-sm font-medium transition-colors"
			>
				Back to the signal
			</a>
			<a
				href="mailto:support@notifbuddy.com"
				class="text-muted-foreground/70 hover:text-muted-foreground font-mono text-[11px] tracking-[0.12em] underline-offset-2 hover:underline"
			>
				report a broken link
			</a>
		</div>
	</main>

	<footer class="mx-auto flex w-full max-w-6xl items-center justify-between px-4 py-6 sm:px-6">
		<p class="text-muted-foreground/70 font-mono text-[11px] tracking-[0.12em]">
			© 2026 notifbuddy — all the noise, one signal
		</p>
		<a
			class="text-muted-foreground/70 hover:text-muted-foreground font-mono text-[11px] tracking-[0.12em] underline-offset-2 hover:underline"
			href={privacyHref}>privacy</a
		>
	</footer>
</div>

<style>
	/* Same soft clay radial as the landing hero, one hue only. */
	.landing {
		background:
			radial-gradient(
				110% 70% at 70% 0%,
				color-mix(in oklab, var(--primary) 7%, transparent),
				transparent 60%
			),
			var(--background);
	}

	.eyebrow {
		color: color-mix(in oklab, var(--foreground) 60%, transparent);
		display: inline-flex;
		align-items: center;
		gap: 0.6rem;
	}
	/* The brand's signal dot, breathing slowly — same pulse as the hero pip. */
	.eyebrow-pip {
		width: 7px;
		height: 7px;
		border-radius: 999px;
		background: var(--primary);
		box-shadow: 0 0 10px color-mix(in oklab, var(--primary) 70%, transparent);
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

	/* Lost-signal diagram: quiet foreground linework, clay reserved for the dot. */
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
	@media (prefers-reduced-motion: reduce) {
		.eyebrow-pip,
		.lost-signal .dot {
			animation: none;
		}
	}
</style>
