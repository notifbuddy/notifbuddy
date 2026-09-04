<script lang="ts">
	import Logo from '$shared/components/logo.svelte';
	import StatusBadge from '$shared/components/status-badge.svelte';
	import LinearIcon from '$lib/icons/linear.svelte';
	import GithubIcon from '$lib/icons/github.svelte';
	import SlackIcon from '$lib/icons/slack.svelte';
	import PlainIcon from '$lib/icons/plain.svelte';
	import { userPrefersMode, setMode, mode } from 'mode-watcher';

	// Theme switcher: one quiet icon button cycling system → light → dark.
	// System is the default; the icon shows the current preference and the
	// tooltip names it plus what a click switches to.
	const themeOrder = ['system', 'light', 'dark'] as const;
	const themeLabel = { system: 'System theme', light: 'Light theme', dark: 'Dark theme' };
	const themeNext = $derived(
		themeOrder[(themeOrder.indexOf(userPrefersMode.current) + 1) % themeOrder.length]
	);
	function cycleTheme() {
		setMode(themeNext);
	}

	// The demo clip is authored at 1920×1080 (a HyperFrames composition in
	// static/demo/sync.html); scale it to whatever width the panel gets.
	let clipWidth = $state(0);
	let replyClipWidth = $state(0);

	const faqs = [
		{
			q: 'What does notifbuddy do?',
			a: 'notifbuddy opens a Slack channel for each Linear issue and mirrors updates both ways. Comments, status changes, and assignments land in Slack as they happen, and replies written in Slack land back on the issue — nothing gets lost and nothing pings twice.'
		},
		{
			q: 'Which tools does notifbuddy connect?',
			a: 'Linear and Slack sync is live today. GitHub, Plain, and Discord support is rolling out during the beta.'
		},
		{
			q: 'Is notifbuddy free?',
			a: 'Yes — notifbuddy is free while in open beta. No credit card required: sign up and start syncing right away.'
		},
		{
			q: "How is this different from Linear's built-in Slack integration?",
			a: 'Built-in integrations push notifications one way into shared channels. notifbuddy gives every issue its own channel, collapses duplicate updates into one signal, and syncs your replies back into the tool they came from.'
		},
		{
			q: 'Is notifbuddy open source?',
			a: 'Yes. The code lives on GitHub under the notifbuddy organization, and a community Slack is open for feedback.'
		},
		{
			q: 'Can I self-host notifbuddy?',
			a: 'Yes. A Helm chart at ghcr.io/notifbuddy/charts/notifbuddy deploys the dashboard, the API, and the auth service on your own Kubernetes cluster. The self-hosting guide in the docs walks through it.'
		},
		{
			q: 'Do I have to leave the terminal to set it up?',
			a: 'No. Install the CLI with brew install notifbuddy/tap/notifbuddy, run notifbuddy login, and connect Slack and Linear without leaving your terminal — or install the bundled skill and let your coding agent run the whole onboarding.'
		}
	];
</script>

<svelte:head>
	<!-- SERP title leads with search intent; the brand tagline stays the H1 and
	     the og:title (social cards favor the brand voice). -->
	<title>Two-way Slack sync for Linear, GitHub & Plain — notifbuddy</title>
	<meta
		name="description"
		content="notifbuddy moves notifications both ways between Linear, GitHub, Plain and Slack. Now in open beta — free to use."
	/>
	<link rel="canonical" href="https://notifbuddy.com/" />
	<meta property="og:type" content="website" />
	<meta property="og:site_name" content="notifbuddy" />
	<meta property="og:url" content="https://notifbuddy.com/" />
	<meta property="og:title" content="notifbuddy — all the noise, one signal" />
	<meta
		property="og:description"
		content="Two-way notification sync between Linear, GitHub, Plain and Slack. Now in open beta."
	/>
	<meta property="og:image" content="https://notifbuddy.com/og-card.png" />
	<meta property="og:image:width" content="1200" />
	<meta property="og:image:height" content="630" />
	<meta name="twitter:card" content="summary_large_image" />
	<!-- eslint-disable-next-line svelte/no-at-html-tags -- static, trusted JSON-LD -->
	{@html `<script type="application/ld+json">${JSON.stringify({
		'@context': 'https://schema.org',
		'@graph': [
			{
				'@type': 'Organization',
				name: 'notifbuddy',
				url: 'https://notifbuddy.com/',
				logo: 'https://notifbuddy.com/apple-touch-icon.png',
				slogan: 'All the noise. One signal.'
			},
			{
				'@type': 'WebSite',
				name: 'notifbuddy',
				url: 'https://notifbuddy.com/'
			},
			{
				'@type': 'SoftwareApplication',
				name: 'notifbuddy',
				url: 'https://dashboard.notifbuddy.com',
				applicationCategory: 'BusinessApplication',
				operatingSystem: 'Web',
				description:
					'Two-way notification sync between Linear, GitHub, Plain and Slack.',
				offers: { '@type': 'Offer', price: '0', priceCurrency: 'USD' }
			},
			{
				'@type': 'FAQPage',
				mainEntity: faqs.map((f) => ({
					'@type': 'Question',
					name: f.q,
					acceptedAnswer: { '@type': 'Answer', text: f.a }
				}))
			}
		]
	})}</scr` + `ipt>`}
</svelte:head>

<div class="landing flex min-h-svh flex-col">
	<header class="mx-auto flex w-full max-w-6xl items-center justify-between px-4 py-5 sm:px-6">
		<!-- Below 420px the wordmark + action cluster can't share the row without
		     shrinking tap targets, so the mark stands alone (the dot is the brand). -->
		<span class="hidden min-[420px]:block"><Logo size={30} /></span>
		<span class="min-[420px]:hidden"><Logo size={30} wordmark={false} /></span>
		<div class="flex items-center gap-1.5">
			<a
				href="https://docs.notifbuddy.com"
				class="text-muted-foreground hover:text-foreground focus-visible:ring-ring inline-flex h-9 items-center justify-center rounded-md px-3 text-sm font-medium transition-colors focus-visible:ring-2 focus-visible:outline-none"
				rel="noopener"
			>
				Docs
			</a>
			<a
				href="https://github.com/notifbuddy/notifbuddy"
				class="text-muted-foreground hover:text-foreground focus-visible:ring-ring inline-flex size-9 items-center justify-center rounded-md transition-colors focus-visible:ring-2 focus-visible:outline-none"
				aria-label="notifbuddy on GitHub"
				rel="noopener"
			>
				<GithubIcon size={17} />
			</a>
			<button
				type="button"
				class="theme-btn text-muted-foreground hover:text-foreground relative inline-flex size-9 items-center justify-center rounded-md transition-colors"
				aria-label="{themeLabel[userPrefersMode.current]} — switch to {themeLabel[
					themeNext
				].toLowerCase()}"
				data-tooltip="{themeLabel[userPrefersMode.current]} · click for {themeNext}"
				onclick={cycleTheme}
			>
				{#if userPrefersMode.current === 'light'}
					<!-- sun -->
					<svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
						<circle cx="12" cy="12" r="4" />
						<path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" />
					</svg>
				{:else if userPrefersMode.current === 'dark'}
					<!-- moon -->
					<svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
						<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z" />
					</svg>
				{:else}
					<!-- monitor (system) -->
					<svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
						<rect width="20" height="14" x="2" y="3" rx="2" />
						<path d="M8 21h8M12 17v4" />
					</svg>
				{/if}
			</button>
			<a
				href="https://dashboard.notifbuddy.com"
				class="border-input text-foreground hover:bg-foreground/5 focus-visible:ring-ring ml-1 inline-flex h-9 items-center justify-center rounded-md border px-4 text-sm font-medium transition-colors focus-visible:ring-2 focus-visible:outline-none"
			>
				Log in
			</a>
		</div>
	</header>

	<main
		class="mx-auto grid w-full max-w-6xl flex-1 items-center gap-10 px-4 py-10 sm:px-6 lg:grid-cols-[1fr_1.2fr] lg:gap-14"
	>
		<!-- Left: the pitch, kept short — this page has one job. -->
		<section class="flex max-w-lg flex-col gap-6">
			<p class="eyebrow font-mono text-xs tracking-[0.18em] uppercase">
				<span class="eyebrow-pip" aria-hidden="true"></span>
				open beta
			</p>
			<h1 class="text-4xl leading-[1.05] font-semibold tracking-tight text-balance sm:text-5xl">
				All the noise.<br />One signal<span class="text-primary">.</span>
			</h1>
			<p class="text-muted-foreground text-base leading-relaxed text-pretty">
				notifbuddy moves notifications both ways — Linear, GitHub, and Plain into one calm Slack
				feed, and your Slack replies back into the tool they came from. Nothing gets lost, nothing
				pings twice.
			</p>

			<div class="flex flex-col gap-2 sm:flex-row sm:items-center">
				<a
					href="https://dashboard.notifbuddy.com"
					class="bg-primary text-primary-foreground hover:bg-primary/90 focus-visible:ring-ring inline-flex h-10 items-center justify-center rounded-md px-5 text-sm font-medium transition-colors focus-visible:ring-2 focus-visible:outline-none"
				>
					Start syncing
				</a>
			</div>
			<p class="text-muted-foreground/80 -mt-3 text-xs">
				Free while in beta — no credit card, sign up and go.
			</p>
		</section>

		<!-- Right: the demo clip. A HyperFrames composition (static/demo/sync.html)
		     framed like a product video: chrome bar on top with the synced
		     products, the 1920×1080 canvas scaled into place below. -->
		<section class="clip overflow-hidden rounded-xl border">
			<div class="clip-chrome flex items-center justify-between gap-3 border-b px-4 py-2.5">
				<p
					class="text-muted-foreground flex min-w-0 items-center gap-2 font-mono text-[11px] tracking-[0.12em]"
				>
					<span class="hidden items-center gap-1.5 sm:flex" aria-hidden="true">
						<LinearIcon size={13} />
						<GithubIcon size={13} />
						<PlainIcon size={13} />
					</span>
					<span class="truncate">linear · github · plain <span class="text-primary">⇄</span> slack</span>
					<SlackIcon size={13} aria-hidden="true" />
				</p>
				<p class="text-muted-foreground flex items-center gap-1.5 font-mono text-[11px] tracking-[0.12em]">
					<span class="live-dot" aria-hidden="true"></span>
					live
				</p>
			</div>
			<div class="clip-canvas relative" bind:clientWidth={clipWidth}>
				{#if clipWidth > 0}
					<iframe
						src="/demo/sync.html"
						title="Demo: notifications syncing both ways between Linear, GitHub, Plain and Slack"
						loading="lazy"
						style="transform: scale({clipWidth / 1920});"
					></iframe>
				{/if}
			</div>
		</section>
	</main>

	<section class="mx-auto w-full max-w-6xl px-4 py-20 sm:px-6" aria-labelledby="features-h">
		<p class="eyebrow font-mono text-xs tracking-[0.18em] uppercase">
			<span class="eyebrow-pip" aria-hidden="true"></span>
			both directions
		</p>
		<h2 id="features-h" class="mt-4 max-w-xl text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
			One channel per issue. One signal per update.
		</h2>
		<div class="mt-10 grid gap-5 sm:grid-cols-3">
			<div class="feature-card rounded-xl border p-6">
				<h3 class="font-medium">Into Slack</h3>
				<p class="text-muted-foreground mt-2 text-sm leading-relaxed">
					Every issue gets its own channel. Comments, status changes, and assignments arrive as they
					happen — no digest lag, no shared-channel firehose.
				</p>
			</div>
			<div class="feature-card rounded-xl border p-6">
				<h3 class="font-medium">Back out again</h3>
				<p class="text-muted-foreground mt-2 text-sm leading-relaxed">
					Reply in the channel and it lands on the issue as a comment, attributed to you. Your team
					reads it where they work, you never left Slack.
				</p>
			</div>
			<div class="feature-card rounded-xl border p-6">
				<h3 class="font-medium">Your rules</h3>
				<p class="text-muted-foreground mt-2 text-sm leading-relaxed">
					Per-team rules decide when a channel opens, how it's named, and who's added
					automatically. Set once, then stop thinking about it.
				</p>
			</div>
		</div>
	</section>

	<section class="mx-auto w-full max-w-6xl px-4 py-20 sm:px-6" aria-labelledby="reply-h">
		<div class="grid items-center gap-10 lg:grid-cols-[1fr_1.2fr] lg:gap-14">
			<div class="flex max-w-lg flex-col gap-5">
				<p class="eyebrow font-mono text-xs tracking-[0.18em] uppercase">
					<span class="eyebrow-pip" aria-hidden="true"></span>
					reply anywhere
				</p>
				<h2 id="reply-h" class="text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
					Written in Slack. Filed in Linear.
				</h2>
				<p class="text-muted-foreground text-base leading-relaxed text-pretty">
					A reply typed in the issue's channel travels back through notifbuddy and lands on the
					issue itself — comment, attribution, status and all. The conversation stays whole in both
					places.
				</p>
			</div>
			<div class="clip overflow-hidden rounded-xl border">
				<div class="clip-chrome flex items-center justify-between gap-3 border-b px-4 py-2.5">
					<p class="text-muted-foreground flex min-w-0 items-center gap-2 font-mono text-[11px] tracking-[0.12em]">
						<SlackIcon size={13} aria-hidden="true" />
						<span class="truncate">slack <span class="text-primary">⇄</span> linear · replies</span>
						<LinearIcon size={13} aria-hidden="true" />
					</p>
					<p class="text-muted-foreground flex items-center gap-1.5 font-mono text-[11px] tracking-[0.12em]">
						<span class="live-dot" aria-hidden="true"></span>
						live
					</p>
				</div>
				<div class="clip-canvas relative" bind:clientWidth={replyClipWidth}>
					{#if replyClipWidth > 0}
						<iframe
							src="/demo/reply.html"
							title="Demo: a reply written in Slack syncing back to the Linear issue"
							loading="lazy"
							style="transform: scale({replyClipWidth / 1920});"
						></iframe>
					{/if}
				</div>
			</div>
		</div>
	</section>

	<section class="mx-auto w-full max-w-6xl px-4 py-20 sm:px-6" aria-labelledby="how-h">
		<p class="eyebrow font-mono text-xs tracking-[0.18em] uppercase">
			<span class="eyebrow-pip" aria-hidden="true"></span>
			how it works
		</p>
		<h2 id="how-h" class="mt-4 text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
			Syncing in three steps
		</h2>
		<ol class="mt-10 grid gap-8 sm:grid-cols-3">
			<li class="flex flex-col gap-2">
				<span class="text-primary font-mono text-sm tracking-[0.18em]">01</span>
				<h3 class="font-medium">Connect</h3>
				<p class="text-muted-foreground text-sm leading-relaxed">
					Sign in with GitHub, then connect Slack and Linear — from the dashboard, or without
					leaving your terminal via the CLI.
				</p>
			</li>
			<li class="flex flex-col gap-2">
				<span class="text-primary font-mono text-sm tracking-[0.18em]">02</span>
				<h3 class="font-medium">Set channel rules</h3>
				<p class="text-muted-foreground text-sm leading-relaxed">
					Per Linear team: when channels open, how they're named, and who gets added automatically.
				</p>
			</li>
			<li class="flex flex-col gap-2">
				<span class="text-primary font-mono text-sm tracking-[0.18em]">03</span>
				<h3 class="font-medium">Stay in Slack</h3>
				<p class="text-muted-foreground text-sm leading-relaxed">
					Updates flow in, replies flow back. Both tools stay current without you tabbing between
					them.
				</p>
			</li>
		</ol>
	</section>

	<section class="mx-auto w-full max-w-6xl px-4 py-20 sm:px-6" aria-labelledby="oss-h">
		<div class="grid items-center gap-10 lg:grid-cols-2">
			<div class="flex max-w-lg flex-col gap-5">
				<p class="eyebrow font-mono text-xs tracking-[0.18em] uppercase">
					<span class="eyebrow-pip" aria-hidden="true"></span>
					open source
				</p>
				<h2 id="oss-h" class="text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
					Yours to read. Yours to run.
				</h2>
				<p class="text-muted-foreground text-base leading-relaxed text-pretty">
					notifbuddy is open source, and a Helm chart deploys the whole thing — dashboard, API, auth
					— on your own cluster. Or skip the ops and use the hosted beta.
				</p>
				<div class="flex flex-wrap items-center gap-4">
					<a
						href="https://github.com/notifbuddy/notifbuddy"
						class="border-input text-foreground hover:bg-foreground/5 focus-visible:ring-ring inline-flex h-9 items-center justify-center gap-2 rounded-md border px-4 text-sm font-medium transition-colors focus-visible:ring-2 focus-visible:outline-none"
						rel="noopener"
					>
						<GithubIcon size={15} />
						Star on GitHub
					</a>
					<a
						href="https://docs.notifbuddy.com/self-host"
						class="text-muted-foreground hover:text-foreground font-mono text-xs tracking-[0.12em] underline-offset-2 hover:underline"
						rel="noopener"
					>
						self-hosting guide
					</a>
				</div>
			</div>
			<pre class="terminal overflow-x-auto rounded-xl border p-5 font-mono text-xs leading-relaxed"><code
					><span class="text-muted-foreground"># hosted, from your terminal</span>
brew install notifbuddy/tap/notifbuddy
notifbuddy login

<span class="text-muted-foreground"># or on your own cluster</span>
helm install notifbuddy \
  oci://ghcr.io/notifbuddy/charts/notifbuddy \
  --namespace notifbuddy --create-namespace</code
				></pre>
		</div>
	</section>

	<section class="faq mx-auto w-full max-w-3xl px-4 py-20 sm:px-6" aria-labelledby="faq-h">
		<p class="eyebrow font-mono text-xs tracking-[0.18em] uppercase">
			<span class="eyebrow-pip" aria-hidden="true"></span>
			faq
		</p>
		<h2 id="faq-h" class="mt-4 text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
			Frequently asked questions
		</h2>
		<div class="mt-8">
			{#each faqs as faq (faq.q)}
				<details>
					<summary>{faq.q}</summary>
					<p class="answer text-sm">{faq.a}</p>
				</details>
			{/each}
		</div>
	</section>

	<section class="mx-auto flex w-full max-w-6xl flex-col items-center gap-6 px-4 py-24 text-center sm:px-6">
		<h2 class="text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
			All the noise. One signal<span class="text-primary">.</span>
		</h2>
		<a
			href="https://dashboard.notifbuddy.com"
			class="bg-primary text-primary-foreground hover:bg-primary/90 focus-visible:ring-ring inline-flex h-10 items-center justify-center rounded-md px-5 text-sm font-medium transition-colors focus-visible:ring-2 focus-visible:outline-none"
		>
			Start syncing
		</a>
		<p class="text-muted-foreground/80 text-xs">Free while in beta — no credit card, sign up and go.</p>
	</section>

	<footer
		class="mx-auto flex w-full max-w-6xl flex-col gap-2 px-4 py-6 sm:flex-row sm:items-center sm:justify-between sm:px-6"
	>
		<p class="text-muted-foreground/70 font-mono text-[11px] tracking-[0.12em]">
			© 2026 notifbuddy — all the noise, one signal
		</p>
		<span class="flex flex-wrap items-center gap-x-4 gap-y-3">
			<StatusBadge
				theme={mode.current === 'dark' ? 'dark' : 'light'}
				label="status"
				linkClass="text-muted-foreground/70 hover:text-muted-foreground font-mono text-[11px] tracking-[0.12em] underline-offset-2 hover:underline"
			/>
			<a
				class="text-muted-foreground/70 hover:text-muted-foreground font-mono text-[11px] tracking-[0.12em] underline-offset-2 hover:underline"
				href="https://docs.notifbuddy.com/changelog">changelog</a
			>
			<a
				class="text-muted-foreground/70 hover:text-muted-foreground font-mono text-[11px] tracking-[0.12em] underline-offset-2 hover:underline"
				href="https://docs.notifbuddy.com">docs</a
			>
			<a
				class="text-muted-foreground/70 hover:text-muted-foreground font-mono text-[11px] tracking-[0.12em] underline-offset-2 hover:underline"
				href="/privacy">privacy</a
			>
		</span>
	</footer>
</div>

<style>
	/* One soft clay radial on the brand background, no second hue. */
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
	/* The brand's signal dot, breathing slowly. */
	.eyebrow-pip {
		width: 7px;
		height: 7px;
		border-radius: 999px;
		background: var(--primary);
		box-shadow: 0 0 10px color-mix(in oklab, var(--primary) 70%, transparent);
		animation: pip-breathe 3.5s ease-in-out infinite;
	}
	.live-dot {
		width: 6px;
		height: 6px;
		border-radius: 999px;
		background: var(--primary);
		animation: pip-breathe 2.5s ease-in-out infinite;
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

	/* Hand-rolled tooltip for the theme button (this site carries no UI-kit
	   dependency): a small themed chip below the button on hover/focus. */
	.theme-btn::after {
		content: attr(data-tooltip);
		position: absolute;
		top: calc(100% + 8px);
		right: 0;
		white-space: nowrap;
		font-family: var(--font-mono);
		font-size: 11px;
		letter-spacing: 0.04em;
		color: var(--foreground);
		background: color-mix(in oklab, var(--card) 96%, transparent);
		border: 1px solid color-mix(in oklab, var(--foreground) 14%, transparent);
		border-radius: var(--radius-sm);
		padding: 4px 8px;
		opacity: 0;
		translate: 0 -2px;
		pointer-events: none;
		transition:
			opacity 0.15s ease,
			translate 0.15s ease;
		z-index: 10;
	}
	.theme-btn:hover::after,
	.theme-btn:focus-visible::after {
		opacity: 1;
		translate: 0 0;
	}

	.clip {
		border-color: color-mix(in oklab, var(--foreground) 12%, transparent);
		background: #201f1c; /* the clip's own ink canvas */
		box-shadow: 0 24px 60px -28px rgb(0 0 0 / 0.7);
	}
	.clip-chrome {
		border-color: color-mix(in oklab, var(--foreground) 10%, transparent);
		background: color-mix(in oklab, var(--card) 92%, transparent);
	}
	/* The 11px mono labels ride text-muted-foreground, which lands under the
	   4.5:1 WCAG floor on the light chrome bar — pull them toward foreground
	   (solid mix, no alpha, so contrast holds in both themes). */
	.clip-chrome p {
		color: color-mix(in oklab, var(--foreground) 80%, var(--card));
	}
	.clip-canvas {
		aspect-ratio: 16 / 9;
	}
	.clip-canvas iframe {
		position: absolute;
		top: 0;
		left: 0;
		width: 1920px;
		height: 1080px;
		border: 0;
		transform-origin: top left;
	}

	.feature-card,
	.terminal {
		border-color: color-mix(in oklab, var(--foreground) 12%, transparent);
		background: color-mix(in oklab, var(--card) 55%, transparent);
	}
	.terminal {
		color: var(--foreground);
	}

	.faq details {
		border-bottom: 1px solid color-mix(in oklab, var(--foreground) 10%, transparent);
	}
	.faq summary {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
		padding: 1.1rem 0;
		font-weight: 500;
		cursor: pointer;
		list-style: none;
	}
	.faq summary::-webkit-details-marker {
		display: none;
	}
	.faq summary::after {
		content: '+';
		font-family: var(--font-mono);
		font-size: 1.05rem;
		color: var(--primary);
		transition: rotate 0.2s ease;
	}
	.faq details[open] summary::after {
		rotate: 45deg;
	}
	.faq .answer {
		max-width: 65ch;
		padding-bottom: 1.25rem;
		line-height: 1.65;
		color: var(--muted-foreground);
	}

	@media (prefers-reduced-motion: reduce) {
		.eyebrow-pip,
		.live-dot {
			animation: none;
		}
	}
</style>
