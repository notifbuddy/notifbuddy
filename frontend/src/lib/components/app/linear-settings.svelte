<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import * as Card from '$lib/components/ui/card';
	import { Input } from '$lib/components/ui/input';
	import { Badge } from '$lib/components/ui/badge';
	import { SiLinear } from '@icons-pack/svelte-simple-icons';
	import LoaderIcon from '@lucide/svelte/icons/loader-circle';
	import PlayIcon from '@lucide/svelte/icons/play';
	import SaveIcon from '@lucide/svelte/icons/save';
	import XIcon from '@lucide/svelte/icons/x';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import CheckIcon from '@lucide/svelte/icons/check';
	import {
		fetchLinearSettings,
		saveLinearSettings,
		testLinearTemplate,
		type LinearSettings,
		type SampleEvent,
		type TemplateTestResult
	} from '$lib/integrations';

	let ctx = $state<{
		connected: boolean;
		sampleEvents: SampleEvent[];
	} | null>(null);
	let loading = $state(true);

	// Editable settings (a working copy).
	let creationMode = $state<'status' | 'manual'>('manual');
	let triggerStatus = $state('');
	let nameTemplate = $state('');
	let conditionExpr = $state('');
	let bots = $state<string[]>([]);
	let newBot = $state('');

	let saving = $state(false);
	let saveMsg = $state<string | null>(null);

	// Test panel.
	let sampleId = $state('');
	let pastedEvent = $state('');
	let testing = $state(false);
	let testResult = $state<TemplateTestResult | null>(null);

	async function load() {
		loading = true;
		const s = await fetchLinearSettings();
		loading = false;
		if (!s) return;
		ctx = { connected: s.connected, sampleEvents: s.sampleEvents };
		creationMode = s.settings.creationMode;
		triggerStatus = s.settings.triggerStatus ?? '';
		nameTemplate = s.settings.nameTemplate ?? '';
		conditionExpr = s.settings.conditionExpr ?? '';
		bots = [...(s.settings.autoAddBots ?? [])];
		if (s.sampleEvents.length) sampleId = s.sampleEvents[0].id;
	}
	load();

	function addBot() {
		const v = newBot.trim();
		if (v && !bots.includes(v)) bots = [...bots, v];
		newBot = '';
	}
	function removeBot(b: string) {
		bots = bots.filter((x) => x !== b);
	}

	async function doSave() {
		saving = true;
		saveMsg = null;
		const payload: LinearSettings = {
			creationMode,
			triggerStatus: triggerStatus || undefined,
			nameTemplate: nameTemplate || undefined,
			conditionExpr: conditionExpr || undefined,
			autoAddBots: bots
		};
		const next = await saveLinearSettings(payload);
		saving = false;
		saveMsg = next ? 'Saved.' : 'Save failed — check your template/condition.';
		if (next) ctx = { connected: next.connected, sampleEvents: next.sampleEvents };
	}

	async function doTest() {
		testing = true;
		testResult = null;
		const req = pastedEvent.trim()
			? { nameTemplate, condition: conditionExpr, event: pastedEvent }
			: { nameTemplate, condition: conditionExpr, sampleId };
		testResult = await testLinearTemplate(req);
		testing = false;
	}

	const selectCls =
		'border-input bg-background ring-offset-background focus-visible:ring-ring h-9 w-full rounded-md border px-3 py-1 text-sm shadow-xs focus-visible:ring-2 focus-visible:outline-none';
	const textareaCls =
		'border-input bg-background ring-offset-background focus-visible:ring-ring min-h-20 w-full rounded-md border px-3 py-2 font-mono text-xs shadow-xs focus-visible:ring-2 focus-visible:outline-none';
</script>

{#if loading}
	<Card.Root>
		<Card.Header>
			<Card.Title class="flex items-center gap-2 text-base">
				<SiLinear class="size-4" /> Linear workspace settings
			</Card.Title>
		</Card.Header>
		<Card.Content><p class="text-muted-foreground text-sm">Loading…</p></Card.Content>
	</Card.Root>
{:else if ctx && ctx.connected}
	<Card.Root>
		<Card.Header>
			<Card.Title class="flex items-center gap-2 text-base">
				<SiLinear class="size-4" /> Linear workspace settings
			</Card.Title>
			<Card.Description>
				Control how Linear issues create Slack channels. Templates use GitHub Actions expression
				syntax (<code>${'{{'} linear.data.identifier {'}}'}</code>).
			</Card.Description>
		</Card.Header>
		<Card.Content class="flex flex-col gap-5">
			<!-- Creation mode -->
			<div class="flex flex-col gap-2">
				<span class="text-sm font-medium">Channel creation</span>
				<div class="flex gap-2">
					<Button
						variant={creationMode === 'manual' ? 'default' : 'outline'}
						size="sm"
						onclick={() => (creationMode = 'manual')}>Manual (@notifbuddy)</Button
					>
					<Button
						variant={creationMode === 'status' ? 'default' : 'outline'}
						size="sm"
						onclick={() => (creationMode = 'status')}>On issue status</Button
					>
				</div>
				{#if creationMode === 'status'}
					<label class="text-muted-foreground mt-1 text-xs" for="trigger-status">
						Trigger status (Linear workflow state name)
					</label>
					<Input id="trigger-status" bind:value={triggerStatus} placeholder="In Progress" />
				{/if}
			</div>

			<!-- Name template -->
			<div class="flex flex-col gap-1.5">
				<label class="text-sm font-medium" for="name-template">Channel name template</label>
				<Input id="name-template" bind:value={nameTemplate} class="font-mono text-xs"
					placeholder="tkt-${'{{'} linear.data.identifier {'}}'}" />
			</div>

			<!-- Condition -->
			<div class="flex flex-col gap-1.5">
				<label class="text-sm font-medium" for="condition">Creation condition</label>
				<textarea
					id="condition"
					bind:value={conditionExpr}
					class={textareaCls}
					placeholder="linear.action == 'update' && linear.data.state.name == 'Done'"
				></textarea>
				<span class="text-muted-foreground text-xs">Must evaluate to true for a channel to be created.</span>
			</div>

			<!-- Bots -->
			<div class="flex flex-col gap-2">
				<span class="text-sm font-medium">Auto-add bots</span>
				<div class="flex flex-wrap items-center gap-1.5">
					{#each bots as b (b)}
						<Badge variant="secondary" class="gap-1">
							{b}
							<button type="button" aria-label={`Remove ${b}`} onclick={() => removeBot(b)}>
								<XIcon class="size-3" />
							</button>
						</Badge>
					{/each}
					{#if bots.length === 0}
						<span class="text-muted-foreground text-xs">No bots added.</span>
					{/if}
				</div>
				<div class="flex gap-2">
					<Input
						bind:value={newBot}
						placeholder="claude"
						class="h-8 max-w-48"
						onkeydown={(e) => e.key === 'Enter' && (e.preventDefault(), addBot())}
					/>
					<Button size="icon-sm" variant="outline" onclick={addBot} aria-label="Add bot">
						<PlusIcon />
					</Button>
				</div>
			</div>

			<!-- Save -->
			<div class="flex items-center gap-3">
				<Button size="sm" onclick={doSave} disabled={saving}>
					{#if saving}<LoaderIcon class="animate-spin" />{:else}<SaveIcon />{/if}
					Save
				</Button>
				{#if saveMsg}<span class="text-muted-foreground text-sm">{saveMsg}</span>{/if}
			</div>

			<!-- Test panel -->
			<div class="border-t pt-4">
				<span class="text-sm font-medium">Test against an event</span>
				<p class="text-muted-foreground mt-0.5 text-xs">
					Pick a sample event or paste a raw event to validate your template + condition.
				</p>
				<div class="mt-2 flex flex-col gap-2">
					<select bind:value={sampleId} class={selectCls} disabled={!!pastedEvent.trim()}>
						{#each ctx.sampleEvents as s (s.id)}
							<option value={s.id}>{s.label}</option>
						{/each}
					</select>
					<textarea
						bind:value={pastedEvent}
						class={textareaCls}
						placeholder={'Or paste a raw event JSON: { "event_type": "linear", "linear": { ... } }'}
					></textarea>
					<div>
						<Button size="sm" variant="outline" onclick={doTest} disabled={testing}>
							{#if testing}<LoaderIcon class="animate-spin" />{:else}<PlayIcon />{/if}
							Run test
						</Button>
					</div>
				</div>

				{#if testResult}
					<div class="bg-muted/40 mt-3 flex flex-col gap-2 rounded-md border p-3 text-sm">
						{#if testResult.error}
							<p class="text-destructive font-mono text-xs">{testResult.error}</p>
						{:else}
							<div class="flex items-center gap-2">
								<span class="text-muted-foreground">Channel name:</span>
								<code class="font-mono">{testResult.name || '(empty)'}</code>
							</div>
							<div class="flex items-center gap-2">
								<span class="text-muted-foreground">Condition:</span>
								{#if testResult.conditionResult}
									<Badge variant="secondary" class="gap-1"><CheckIcon class="size-3" /> true</Badge>
								{:else}
									<Badge variant="outline" class="gap-1"><XIcon class="size-3" /> false</Badge>
								{/if}
							</div>
						{/if}
					</div>
				{/if}
			</div>
		</Card.Content>
	</Card.Root>
{/if}
