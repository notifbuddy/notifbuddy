<script lang="ts">
	// Quiet signal-interrupted board for the dashboard — Empty + Button + Badge
	// (shadcn-svelte). Used by /interrupted (API OAuth failures) and +error.svelte.
	import Logo from '$shared/components/logo.svelte';
	import * as Empty from '$lib/components/ui/empty/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';

	let {
		status,
		code = null,
		title,
		detail,
		homeHref = '/',
		ctaLabel = 'Back to the signal',
		privacyHref = 'https://notifbuddy.com/privacy',
		supportLabel = 'reach out to support'
	}: {
		status: number;
		code?: string | null;
		title: string;
		detail: string;
		homeHref?: string;
		ctaLabel?: string;
		privacyHref?: string;
		supportLabel?: string;
	} = $props();
</script>

<div class="interrupted flex min-h-svh flex-col">
	<header class="mx-auto flex w-full max-w-6xl items-center px-4 py-5 sm:px-6">
		<a href="/" aria-label="notifbuddy home"><Logo size={30} /></a>
	</header>

	<main class="mx-auto flex w-full max-w-xl flex-1 flex-col items-center justify-center px-4 py-10 sm:px-6">
		<Empty.Root class="border-0 p-0">
			<Empty.Header>
				<Empty.Media class="mb-4 w-full max-w-sm">
					<svg
						class="lost-signal w-full"
						viewBox="0 0 420 130"
						fill="none"
						aria-hidden="true"
					>
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
				</Empty.Media>

				<div class="mb-2 flex flex-wrap items-center justify-center gap-2">
					<Badge variant="outline">http {status}</Badge>
					{#if code}
						<Badge variant="secondary">{code}</Badge>
					{/if}
				</div>

				<Empty.Title class="text-balance text-2xl sm:text-3xl">
					{title}<span class="text-primary">.</span>
				</Empty.Title>
				<Empty.Description class="max-w-md text-pretty">
					{detail}
				</Empty.Description>
			</Empty.Header>

			<Empty.Content>
				<div class="flex flex-col items-center gap-2 sm:flex-row">
					<Button href={homeHref} size="lg">{ctaLabel}</Button>
					<Button href="mailto:support@notifbuddy.com" variant="link" size="sm">
						{supportLabel}
					</Button>
				</div>
			</Empty.Content>
		</Empty.Root>
	</main>

	<footer class="mx-auto flex w-full max-w-6xl items-center justify-between px-4 py-6 sm:px-6">
		<p class="text-muted-foreground/70 font-mono text-[11px] tracking-[0.12em]">
			© 2026 notifbuddy — all the noise, one signal
		</p>
		<Button href={privacyHref} variant="link" size="sm" class="text-muted-foreground/70 h-auto px-0">
			privacy
		</Button>
	</footer>
</div>

<style>
	.interrupted {
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
