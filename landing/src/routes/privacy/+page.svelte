<script lang="ts">
	import Logo from '$lib/components/logo.svelte';
</script>

<svelte:head>
	<title>Privacy — notifbuddy</title>
	<meta
		name="description"
		content="What notifbuddy's waitlist collects (one email address), why, and how to be removed."
	/>
	<link rel="canonical" href="https://notifbuddy.com/privacy" />
</svelte:head>

<div class="flex min-h-svh flex-col">
	<header class="mx-auto flex w-full max-w-6xl items-center px-4 py-5 sm:px-6">
		<a href="/" aria-label="notifbuddy home"><Logo size={30} /></a>
	</header>

	<main class="mx-auto w-full max-w-xl flex-1 px-4 py-10 sm:px-6">
		<h1 class="text-2xl font-semibold tracking-tight">Privacy</h1>
		<div class="text-muted-foreground mt-6 flex flex-col gap-4 text-sm leading-relaxed">
			<p>
				This page collects exactly one thing: the email address you type into the waitlist form.
				We use it for exactly one thing — sending you a single email when notifbuddy opens its
				doors.
			</p>
			<p>
				The address is stored in our own database. It is never sold, shared, or used for anything
				beyond that launch email. This site sets no analytics or tracking cookies; the only
				browser storage used is your theme preference, kept on your device.
			</p>
			<p>
				To be removed from the list, reply to any email we send you, or write to
				<a class="text-foreground underline underline-offset-2" href="mailto:support@notifbuddy.com"
					>support@notifbuddy.com</a
				> and we'll delete your address.
			</p>
		</div>
	</main>

	<footer class="mx-auto w-full max-w-6xl px-4 py-6 sm:px-6">
		<p class="text-muted-foreground/70 font-mono text-[11px] tracking-[0.12em]">
			© 2026 notifbuddy — all the noise, one signal
		</p>
	</footer>
</div>
