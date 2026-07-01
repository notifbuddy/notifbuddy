<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import * as Card from '$lib/components/ui/card';
	import * as Select from '$lib/components/ui/select';
	import * as Field from '$lib/components/ui/field';
	import * as ToggleGroup from '$lib/components/ui/toggle-group';
	import { Input } from '$lib/components/ui/input';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Badge } from '$lib/components/ui/badge';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import LoaderIcon from '@lucide/svelte/icons/loader-circle';
	import PlayIcon from '@lucide/svelte/icons/play';
	import SaveIcon from '@lucide/svelte/icons/save';
	import XIcon from '@lucide/svelte/icons/x';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import CheckIcon from '@lucide/svelte/icons/check';
	import PlugIcon from '@lucide/svelte/icons/plug';
	import { SiLinear } from '@icons-pack/svelte-simple-icons';
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

	// Label shown in the Select trigger for the chosen sample event.
	const sampleLabel = $derived(
		ctx?.sampleEvents.find((s) => s.id === sampleId)?.label ?? 'Select a sample event'
	);

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

</script>

{#if loading}
	<!-- Skeleton mirroring the loaded card: header + the two-region form. -->
	<Card.Root>
		<Card.Header>
			<Skeleton class="h-5 w-32" />
			<Skeleton class="h-4 w-full max-w-md" />
		</Card.Header>
		<Card.Content>
			<div class="grid gap-8 lg:grid-cols-2">
				<div class="flex flex-col gap-5">
					{#each [0, 1, 2] as i (i)}
						<div class="flex flex-col gap-2">
							<Skeleton class="h-4 w-28" />
							<Skeleton class="h-9 w-full" />
						</div>
					{/each}
				</div>
				<div class="flex flex-col gap-3">
					<Skeleton class="h-4 w-40" />
					<Skeleton class="h-9 w-full" />
					<Skeleton class="h-24 w-full" />
					<Skeleton class="h-9 w-24" />
				</div>
			</div>
		</Card.Content>
	</Card.Root>
{:else if ctx && ctx.connected}
	<Card.Root>
		<Card.Header>
			<Card.Title class="text-base">Channel rules</Card.Title>
			<Card.Description>
				Control how Linear issues open Slack channels. Templates and conditions use GitHub Actions
				expression syntax, e.g. <code class="text-xs">${'{{'} linear.data.identifier {'}}'}</code>.
			</Card.Description>
		</Card.Header>
		<Card.Content>
			<!-- Two regions: the rules form, and a test tool that validates them
			     before you save. Side by side on wide screens, stacked below. -->
			<div class="grid gap-8 lg:grid-cols-2">
				<!-- Rules -->
				<Field.FieldGroup>
					<Field.Field>
						<Field.FieldTitle id="creation-mode-label">Channel creation</Field.FieldTitle>
						<Field.FieldDescription>
							Open a channel automatically when an issue reaches a status, or only when someone
							asks <code class="text-xs">@notifbuddy</code>.
						</Field.FieldDescription>
						<ToggleGroup.Root
							type="single"
							variant="outline"
							spacing={2}
							value={creationMode}
							onValueChange={(v) => v && (creationMode = v as 'manual' | 'status')}
							aria-labelledby="creation-mode-label"
						>
							<ToggleGroup.Item
								value="manual"
								class="data-[state=on]:bg-primary data-[state=on]:text-primary-foreground data-[state=on]:border-primary"
							>
								Manual
							</ToggleGroup.Item>
							<ToggleGroup.Item
								value="status"
								class="data-[state=on]:bg-primary data-[state=on]:text-primary-foreground data-[state=on]:border-primary"
							>
								On issue status
							</ToggleGroup.Item>
						</ToggleGroup.Root>
					</Field.Field>

					{#if creationMode === 'status'}
						<Field.Field>
							<Field.FieldLabel for="trigger-status">Trigger status</Field.FieldLabel>
							<Input id="trigger-status" bind:value={triggerStatus} placeholder="In Progress" />
							<Field.FieldDescription>
								The Linear workflow state name that triggers creation.
							</Field.FieldDescription>
						</Field.Field>
					{/if}

					<Field.FieldSeparator />

					<Field.Field>
						<Field.FieldLabel for="name-template">Channel name</Field.FieldLabel>
						<Input
							id="name-template"
							bind:value={nameTemplate}
							class="font-mono text-xs"
							placeholder="tkt-${'{{'} linear.data.identifier {'}}'}"
						/>
						<Field.FieldDescription>Template for the new channel's name.</Field.FieldDescription>
					</Field.Field>

					<Field.Field>
						<Field.FieldLabel for="condition">Creation condition</Field.FieldLabel>
						<Textarea
							id="condition"
							bind:value={conditionExpr}
							class="min-h-20 font-mono text-xs"
							placeholder={"linear.action == 'update' && linear.data.state.name == 'Done'"}
						/>
						<Field.FieldDescription>
							Must evaluate to true for a channel to be created. Leave empty to always create.
						</Field.FieldDescription>
					</Field.Field>

					<Field.FieldSeparator />

					<Field.Field>
						<Field.FieldLabel for="new-bot">Auto-add bots</Field.FieldLabel>
						<Field.FieldDescription>
							Added to every new channel, by Slack member ID or handle.
						</Field.FieldDescription>
						{#if bots.length > 0}
							<div class="flex flex-wrap items-center gap-1.5">
								{#each bots as b (b)}
									<Badge variant="secondary" class="gap-1">
										{b}
										<button type="button" aria-label={`Remove ${b}`} onclick={() => removeBot(b)}>
											<XIcon class="size-3" />
										</button>
									</Badge>
								{/each}
							</div>
						{/if}
						<div class="flex gap-2">
							<Input
								id="new-bot"
								bind:value={newBot}
								placeholder="claude"
								class="max-w-48"
								onkeydown={(e) => e.key === 'Enter' && (e.preventDefault(), addBot())}
							/>
							<Button size="icon" variant="outline" onclick={addBot} aria-label="Add bot">
								<PlusIcon />
							</Button>
						</div>
					</Field.Field>
				</Field.FieldGroup>

				<!-- Test tool: a peer region, divided from the form by a rule rather than
				     boxed in its own card (no card-in-card). -->
				<div
					class="flex h-fit flex-col gap-3 lg:sticky lg:top-4 lg:border-l lg:pl-8"
				>
					<div class="flex flex-col gap-0.5">
						<span class="text-sm font-medium">Test against an event</span>
						<p class="text-muted-foreground text-xs">
							Preview the channel name and condition against a sample or pasted event before
							saving.
						</p>
					</div>
					<Select.Root type="single" bind:value={sampleId} disabled={!!pastedEvent.trim()}>
						<Select.Trigger class="w-full">{sampleLabel}</Select.Trigger>
						<Select.Content>
							<Select.Group>
								{#each ctx.sampleEvents as s (s.id)}
									<Select.Item value={s.id} label={s.label}>{s.label}</Select.Item>
								{/each}
							</Select.Group>
						</Select.Content>
					</Select.Root>
					<Textarea
						bind:value={pastedEvent}
						class="min-h-24 font-mono text-xs"
						placeholder={'Or paste raw event JSON:\n{ "event_type": "linear", "linear": { … } }'}
					/>
					<Button size="sm" variant="outline" class="w-fit" onclick={doTest} disabled={testing}>
						{#if testing}<LoaderIcon class="animate-spin" />{:else}<PlayIcon />{/if}
						Run test
					</Button>

					{#if testResult}
						<div class="flex flex-col gap-2 border-t pt-3 text-sm">
							{#if testResult.error}
								<p class="text-destructive font-mono text-xs">{testResult.error}</p>
							{:else}
								<div class="flex items-center gap-2">
									<span class="text-muted-foreground text-xs">Channel name</span>
									<code class="font-mono text-xs">{testResult.name || '(empty)'}</code>
								</div>
								<div class="flex items-center gap-2">
									<span class="text-muted-foreground text-xs">Condition</span>
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
			</div>
		</Card.Content>
		<Card.Footer class="gap-3">
			<Button size="sm" onclick={doSave} disabled={saving}>
				{#if saving}<LoaderIcon class="animate-spin" />{:else}<SaveIcon />{/if}
				Save
			</Button>
			{#if saveMsg}<span class="text-muted-foreground text-sm">{saveMsg}</span>{/if}
		</Card.Footer>
	</Card.Root>
{:else}
	<!-- Linear isn't connected at the workspace level, so there are no channel
	     rules to configure. Point the user to the integrations page to connect. -->
	<Card.Root>
		<Card.Content class="flex flex-col items-center gap-4 py-12 text-center">
			<div class="bg-muted text-muted-foreground flex size-12 items-center justify-center rounded-full">
				<SiLinear class="size-6" />
			</div>
			<div class="flex flex-col gap-1">
				<p class="font-medium">Connect Linear and Slack to configure channel rules</p>
				<p class="text-muted-foreground max-w-md text-sm">
					These rules create Slack channels from Linear issues, so both Linear and Slack must be
					connected at the workspace level for your organization.
				</p>
			</div>
			<Button href="/settings/integrations/workspace">
				<PlugIcon data-icon="inline-start" />
				Go to integrations
			</Button>
		</Card.Content>
	</Card.Root>
{/if}
