<script lang="ts">
	// JSON editor for the "test against an event" panel. Shiki gives VS Code's
	// TextMate syntax highlighting; since Shiki only *renders* highlighted HTML
	// (it isn't an editor), we use the classic overlay pattern: a transparent
	// <textarea> sits exactly on top of a highlighted <pre>. You type into the
	// (invisible-text) textarea, and the colored <pre> shows through beneath.
	//
	// Highlighting runs client-only. We create a Shiki highlighter with just the
	// json language + the two themes we need, and the JS regex engine, so we don't
	// ship the oniguruma WASM or the full language/theme bundle.
	import { browser } from '$app/environment';
	import { mode as colorMode } from 'mode-watcher';
	import { getLocation, parseTree, findNodeAtLocation, type Segment } from 'jsonc-parser';
	import { cn } from '$lib/utils';
	import { Button } from '$lib/components/ui/button';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import CheckIcon from '@lucide/svelte/icons/check';

	let { value = $bindable(''), class: className }: { value?: string; class?: string } = $props();

	const DARK_THEME = 'github-dark';
	const LIGHT_THEME = 'github-light';

	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	let highlighter = $state<any>(null);
	let textarea = $state<HTMLTextAreaElement | null>(null);
	let pre = $state<HTMLDivElement | null>(null);

	// Lazy-init the highlighter (client only), scoped to json + our two themes.
	$effect(() => {
		if (!browser || highlighter) return;
		let cancelled = false;
		(async () => {
			const [{ createHighlighter }, { createJavaScriptRegexEngine }] = await Promise.all([
				import('shiki'),
				import('shiki/engine/javascript')
			]);
			const hl = await createHighlighter({
				themes: [DARK_THEME, LIGHT_THEME],
				langs: ['json'],
				engine: createJavaScriptRegexEngine()
			});
			if (!cancelled) highlighter = hl;
		})();
		return () => {
			cancelled = true;
		};
	});

	const theme = $derived(colorMode.current === 'light' ? LIGHT_THEME : DARK_THEME);

	// Rendered highlighted HTML. Always append a newline: a textarea gives a
	// trailing \n a real, caret-reachable empty line, but in the highlighted pre
	// that final empty line is a zero-height line box. The extra \n turns it into
	// a real line so both layers have the same content height — otherwise the
	// scroll ranges diverge and clicks land one line off near the bottom.
	const highlighted = $derived.by(() => {
		if (!highlighter) return '';
		const code = value + '\n';
		return highlighter.codeToHtml(code, {
			lang: 'json',
			theme,
			// Strip Shiki's own background so our container/token bg shows through.
			colorReplacements: {}
		});
	});

	// Keep the highlighted layer scrolled in lockstep with the textarea.
	function syncScroll() {
		if (pre && textarea) {
			pre.scrollTop = textarea.scrollTop;
			pre.scrollLeft = textarea.scrollLeft;
		}
	}

	// The JSON path at the cursor, as a breadcrumb. getLocation is fault-tolerant,
	// so the path keeps resolving even while the JSON is mid-edit / invalid.
	let cursorPath = $state<Segment[]>([]);
	function updateCursorPath() {
		if (!textarea) return;
		try {
			cursorPath = getLocation(value, textarea.selectionStart).path;
		} catch {
			cursorPath = [];
		}
	}

	// The cursor path as a template expression, e.g. linear.actor[0].email —
	// what the copy button puts on the clipboard, ready to paste into the
	// channel-name/condition templates (GitHub Actions expression syntax, so
	// no JSONPath `$.` prefix). Keys that aren't identifier-safe use the
	// expressions' bracket form (['weird key']).
	const pathText = $derived.by(() => {
		let out = '';
		for (const seg of cursorPath) {
			if (typeof seg === 'number') out += `[${seg}]`;
			else if (/^[A-Za-z_$][A-Za-z0-9_$]*$/.test(seg)) out += out ? `.${seg}` : seg;
			else out += `['${seg.replace(/'/g, "\\'")}']`;
		}
		return out;
	});

	let pathCopied = $state(false);
	async function copyPath() {
		await navigator.clipboard.writeText(pathText);
		pathCopied = true;
		setTimeout(() => (pathCopied = false), 2000);
	}

	// Move the cursor to a breadcrumb segment's node. We resolve the exact node by
	// its path prefix (not a text search) so repeated keys like "id"/"createdAt"
	// navigate to the right one.
	function jumpToSegment(index: number) {
		if (!textarea) return;
		const path = cursorPath.slice(0, index + 1);
		let node;
		try {
			const tree = parseTree(value);
			node = tree ? findNodeAtLocation(tree, path) : undefined;
		} catch {
			return;
		}
		if (!node) return;
		textarea.focus();
		textarea.setSelectionRange(node.offset, node.offset + node.length);
		syncScroll();
		updateCursorPath();
	}
</script>

<div class={cn('json-editor', className)} data-theme={theme}>
	<!-- Path breadcrumb: the JSON path at the cursor, e.g. linear › actor › email.
	     Segments are clickable to jump to that key. -->
	<div class="json-editor__breadcrumb">
		{#if cursorPath.length > 0}
			<span class="json-editor__crumb-root">$</span>
			{#each cursorPath as seg, i (i)}
				<span class="json-editor__sep">›</span>
				{#if typeof seg === 'string'}
					<button type="button" class="json-editor__crumb" onclick={() => jumpToSegment(i)}>
						{seg}
					</button>
				{:else}
					<span class="json-editor__crumb json-editor__crumb--index">[{seg}]</span>
				{/if}
			{/each}
			<span class="json-editor__copy">
				<Tooltip.Provider delayDuration={200}>
					<Tooltip.Root>
						<Tooltip.Trigger>
							{#snippet child({ props })}
								<Button
									{...props}
									variant="ghost"
									size="icon-sm"
									class="size-5"
									onclick={copyPath}
									aria-label="Copy path {pathText}"
								>
									{#if pathCopied}
										<CheckIcon />
									{:else}
										<CopyIcon />
									{/if}
								</Button>
							{/snippet}
						</Tooltip.Trigger>
						<Tooltip.Content>{pathCopied ? 'Copied' : 'Copy path'}</Tooltip.Content>
					</Tooltip.Root>
				</Tooltip.Provider>
			</span>
		{:else}
			<span class="json-editor__crumb-root">$</span>
		{/if}
	</div>

	<div class="json-editor__body">
		<!-- Highlighted output (behind). aria-hidden: the textarea is the a11y surface. -->
		<div bind:this={pre} class="json-editor__pre" aria-hidden="true">
			{#if highlighter}
				<!-- eslint-disable-next-line svelte/no-at-html-tags -- Shiki output is trusted -->
				{@html highlighted}
			{/if}
		</div>
		<!-- Transparent editable surface (in front). -->
		<textarea
			bind:this={textarea}
			bind:value
			onscroll={syncScroll}
			onclick={updateCursorPath}
			onkeyup={updateCursorPath}
			onselect={updateCursorPath}
			oninput={updateCursorPath}
			spellcheck="false"
			autocapitalize="off"
			class="json-editor__textarea"
			placeholder={'Paste raw event JSON:\n{ "event_type": "linear", "linear": { … } }'}
		></textarea>
	</div>
</div>

<style>
	.json-editor {
		--je-pad: 0.75rem;
		--je-font: 'JetBrains Mono Variable', monospace;
		--je-size: 0.75rem;
		--je-leading: 1.5;
		overflow: hidden;
		border: 1px solid var(--border);
		border-radius: var(--radius);
		background: var(--muted);
		transition:
			border-color 0.15s,
			box-shadow 0.15s;
	}
	/* Focus the whole editor as one box (border tint + soft ring), matching the
	   app's Input/Textarea focus — not a hard outline around just the code area. */
	.json-editor:focus-within {
		border-color: var(--ring);
		box-shadow: 0 0 0 2px color-mix(in oklch, var(--ring) 30%, transparent);
	}

	/* Path breadcrumb bar on top of the editor. */
	.json-editor__breadcrumb {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 0.125rem;
		border-bottom: 1px solid var(--border);
		padding: 0.375rem 0.625rem;
		font-family: var(--je-font);
		font-size: 0.6875rem;
		line-height: 1.2;
		color: var(--muted-foreground);
	}
	.json-editor__crumb-root {
		color: var(--muted-foreground);
	}
	.json-editor__sep {
		color: var(--muted-foreground);
		opacity: 0.6;
	}
	.json-editor__crumb {
		border-radius: calc(var(--radius) * 0.5);
		padding: 0 0.1875rem;
		color: var(--foreground);
	}
	button.json-editor__crumb:hover {
		background: var(--accent);
	}
	.json-editor__crumb--index {
		color: var(--muted-foreground);
	}
	/* Copy-path button, inline right after the last crumb. The bar's content
	   line is ~16px; the 20px button overlaps the padding instead of growing
	   the bar. */
	.json-editor__copy {
		display: flex;
		align-items: center;
		margin-block: -0.25rem;
		margin-left: 0.25rem;
	}

	/* The editor body: fixed height with internal scroll; the two layers overlap. */
	.json-editor__body {
		position: relative;
		min-height: 12rem;
		max-height: 22rem;
		overflow: hidden;
	}

	.json-editor__pre,
	.json-editor__textarea {
		margin: 0;
		padding: var(--je-pad);
		font-family: var(--je-font);
		font-size: var(--je-size);
		line-height: var(--je-leading);
		tab-size: 2;
		white-space: pre;
		overflow: auto;
		border: 0;
	}

	/* Highlighted layer fills the box, scrolls with the textarea, ignores input. */
	.json-editor__pre {
		position: absolute;
		inset: 0;
		pointer-events: none;
	}
	/* Shiki wraps output in <pre class="shiki"><code>…; neutralize its own bg/pad
	   and let our container provide them. */
	.json-editor__pre :global(pre.shiki) {
		margin: 0;
		padding: 0;
		background: transparent !important;
	}

	/* Editable layer on top: transparent text so the highlight shows through, but
	   a visible caret. Same metrics as the <pre>. */
	.json-editor__textarea {
		/* Block, not the default inline-block: an inline textarea sits on a text
		   baseline, so the body grows a few px taller (descender space) than the
		   textarea itself. The pre (inset: 0) fills that taller box, its scroll
		   range ends up shorter, and syncScroll clamps — misaligning the layers
		   when scrolled to the bottom. */
		display: block;
		position: relative;
		width: 100%;
		height: 100%;
		min-height: 12rem;
		max-height: 22rem;
		resize: none;
		background: transparent;
		color: transparent;
		caret-color: var(--foreground);
		outline: none;
	}
	.json-editor__textarea::placeholder {
		color: var(--muted-foreground);
	}
</style>
