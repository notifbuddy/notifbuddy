<script lang="ts">
	import Logo from '$shared/components/logo.svelte';
</script>

<svelte:head>
	<title>Privacy — notifbuddy</title>
	<meta
		name="description"
		content="What this site collects (nothing personal), how analytics work, and how to reach us."
	/>
	<link rel="canonical" href="https://notifbuddy.com/privacy" />
	<meta property="og:type" content="website" />
	<meta property="og:site_name" content="notifbuddy" />
	<meta property="og:url" content="https://notifbuddy.com/privacy" />
	<meta property="og:title" content="Privacy — notifbuddy" />
	<meta
		property="og:description"
		content="What this site collects (nothing personal), how analytics work, and how to reach us."
	/>
	<meta property="og:image" content="https://notifbuddy.com/og-card.png" />
</svelte:head>

<div class="flex min-h-svh flex-col">
	<header class="chrome-top">
		<div class="chrome-top-inner">
			<a href="/" aria-label="notifbuddy home"><Logo size={30} /></a>
		</div>
	</header>

	<main class="mx-auto w-full max-w-xl flex-1 px-4 py-10 sm:px-6">
		<h1 class="text-2xl font-semibold tracking-tight">Privacy</h1>
		<div class="text-muted-foreground mt-6 flex flex-col gap-4 text-sm leading-relaxed">
			<p>
				This site collects nothing personal. There is no form here anymore — the app itself lives
				at <a
					class="text-foreground underline underline-offset-2"
					href="https://dashboard.notifbuddy.com">dashboard.notifbuddy.com</a
				>, and what it collects is covered when you sign up there.
			</p>
			<p>
				If you joined the waitlist while we had one: that list, including every email address on
				it, was permanently deleted on July 17, 2026. It was never sold, shared, or emailed.
			</p>
			<p>
				For traffic and performance measurement we use Cloudflare Web Analytics, which is
				cookieless and anonymous: it counts page views, referrers, and page speed in aggregate,
				without cookies, persistent identifiers, fingerprinting, or profiles of you. The only
				browser storage this site uses is your theme preference, kept on your device.
			</p>
			<p>
				For anything privacy-related, write to
				<a class="text-foreground underline underline-offset-2" href="mailto:support@notifbuddy.com"
					>support@notifbuddy.com</a
				>.
			</p>
		</div>
	</main>

	<footer class="chrome-bottom mx-auto w-full max-w-6xl px-4 pt-6 sm:px-6">
		<p class="text-muted-foreground/70 font-mono text-[11px] tracking-[0.12em]">
			© 2026 notifbuddy — all the noise, one signal
		</p>
	</footer>
</div>
