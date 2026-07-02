<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import * as Card from '$lib/components/ui/card';
	import * as Select from '$lib/components/ui/select';
	import * as Popover from '$lib/components/ui/popover';
	import * as Collapsible from '$lib/components/ui/collapsible';
	import * as Command from '$lib/components/ui/command';
	import * as Avatar from '$lib/components/ui/avatar';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import * as Field from '$lib/components/ui/field';
	import * as ToggleGroup from '$lib/components/ui/toggle-group';
	import { Input } from '$lib/components/ui/input';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Badge } from '$lib/components/ui/badge';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import LoaderIcon from '@lucide/svelte/icons/loader-circle';
	import PlayIcon from '@lucide/svelte/icons/play';
	import SaveIcon from '@lucide/svelte/icons/save';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import XIcon from '@lucide/svelte/icons/x';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import CheckIcon from '@lucide/svelte/icons/check';
	import PlugIcon from '@lucide/svelte/icons/plug';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import UsersIcon from '@lucide/svelte/icons/users';
	import ChevronsUpDownIcon from '@lucide/svelte/icons/chevrons-up-down';
	import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
	import FlaskConicalIcon from '@lucide/svelte/icons/flask-conical';
	import { SiLinear } from '@icons-pack/svelte-simple-icons';
	import SlackIcon from '$lib/icons/slack.svelte';
	import { cn } from '$lib/utils';
	import {
		fetchLinearSettings,
		createLinearSettings,
		updateLinearSettings,
		deleteLinearSettings,
		syncSettings,
		testLinearTemplate,
		fetchIntegrationStatus,
		statusOf,
		type LinearSettings,
		type LinearTeamState,
		type SlackMember,
		type SampleEvent,
		type TemplateTestResult,
		type IntegrationState
	} from '$lib/integrations';

	// An editable config: the API shape plus a client-only `key` so #each stays
	// stable for unsaved (id-less) rows. A draft's teamId is fixed at add-time —
	// the team is the config's identity, so there's no rename or team-change.
	type Draft = LinearSettings & { key: string };

	let connected = $state(false);
	let teams = $state<LinearTeamState[]>([]);
	let slackMembers = $state<SlackMember[]>([]);
	let sampleEvents = $state<SampleEvent[]>([]);
	let drafts = $state<Draft[]>([]);
	let loading = $state(true);
	let syncing = $state(false);

	const memberName = (id: string) => slackMembers.find((m) => m.memberId === id)?.name ?? id;

	// [experimental] Per-provider status for the empty-state check badges.
	let status = $state<IntegrationState | null>(null);
	const linearConnected = $derived(!!statusOf(status, 'linear')?.connected);
	const slackConnected = $derived(!!statusOf(status, 'slack')?.connected);

	// Per-row transient state, keyed by draft.key.
	let saving = $state<Record<string, boolean>>({});
	let saveMsg = $state<Record<string, string>>({});
	let deleting = $state<Record<string, boolean>>({});

	// Test panel is shared but runs a given draft's template/condition. It's
	// collapsed per config by default (testOpen) and auto-opens the first time a
	// config is edited (autoOpened guards so we only auto-open once — after that
	// the user's manual toggle wins).
	let sampleId = $state('');
	let pastedEvent = $state('');
	let testing = $state<string | null>(null); // draft.key being tested, or null
	let testResult = $state<Record<string, TemplateTestResult>>({});
	let testOpen = $state<Record<string, boolean>>({});
	let autoOpened = $state<Record<string, boolean>>({});

	// Called when any rule field of a config changes: reveal its test panel once.
	function markEdited(d: Draft) {
		if (!autoOpened[d.key]) {
			autoOpened[d.key] = true;
			testOpen[d.key] = true;
		}
	}

	let keySeq = 0;
	const nextKey = () => `draft-${keySeq++}`;

	function toDraft(s: LinearSettings): Draft {
		return {
			key: nextKey(),
			settingId: s.settingId,
			teamId: s.teamId ?? '',
			creationMode: s.creationMode,
			triggerStatus: s.triggerStatus ?? '',
			nameTemplate: s.nameTemplate ?? '',
			conditionExpr: s.conditionExpr ?? '',
			autoAddMembers: [...(s.autoAddMembers ?? [])]
		};
	}

	function applyState(s: {
		connected: boolean;
		configs: LinearSettings[];
		teams: LinearTeamState[];
		slackMembers: SlackMember[];
		sampleEvents: SampleEvent[];
	}) {
		connected = s.connected;
		teams = s.teams ?? [];
		slackMembers = s.slackMembers ?? [];
		sampleEvents = s.sampleEvents ?? [];
		drafts = (s.configs ?? []).map(toDraft);
		// Default the test panel to the first sample event: select it and load its
		// JSON into the editable textarea so there's something to run right away.
		if (sampleEvents.length && !sampleId) selectSample(sampleEvents[0].id);
	}

	// Selecting a sample loads its raw JSON into the editable event textarea.
	function selectSample(id: string) {
		sampleId = id;
		const s = sampleEvents.find((e) => e.id === id);
		if (s) pastedEvent = s.raw;
	}

	async function load() {
		loading = true;
		const s = await fetchLinearSettings();
		loading = false;
		fetchIntegrationStatus().then((st) => (status = st));
		if (s) applyState(s);
	}
	load();

	// Each config applies to exactly one team; the team is its identity.
	const teamName = (id: string) => teams.find((t) => t.teamId === id)?.teamName ?? id;
	const teamKey = (id: string) => teams.find((t) => t.teamId === id)?.teamKey ?? '';

	// Status options are just the config's team's workflow states — no union,
	// since a config maps to one team and statuses are team-specific.
	function statusOptionsFor(d: Draft): string[] {
		const team = teams.find((t) => t.teamId === d.teamId);
		if (!team) return [];
		return [...team.states].map((s) => s.name).sort((a, b) => a.localeCompare(b));
	}

	// Teams that already have a config (draft) — these are excluded from the
	// "add team" picker so you can't create a second config for the same team.
	const configuredTeamIds = $derived(new Set(drafts.map((d) => d.teamId)));
	const availableTeams = $derived(teams.filter((t) => !configuredTeamIds.has(t.teamId)));

	// Open state for the "add team" combobox. Command handles filtering internally.
	let addOpen = $state(false);

	// Add a config panel for a team (defaults to manual creation).
	function addTeam(teamId: string) {
		drafts = [
			...drafts,
			{
				key: nextKey(),
				teamId,
				creationMode: 'manual',
				triggerStatus: '',
				nameTemplate: '',
				conditionExpr: '',
				autoAddMembers: []
			}
		];
		addOpen = false;
	}

	// Toggle a Slack member id in a draft's auto-add list.
	function toggleAutoAdd(d: Draft, id: string) {
		d.autoAddMembers = d.autoAddMembers.includes(id)
			? d.autoAddMembers.filter((x) => x !== id)
			: [...d.autoAddMembers, id];
	}

	// Two-letter initials for an avatar fallback when a member has no icon.
	function initials(name: string): string {
		const parts = name.trim().split(/\s+/);
		const first = parts[0]?.[0] ?? '';
		const second = parts.length > 1 ? (parts[parts.length - 1][0] ?? '') : '';
		return (first + second).toUpperCase() || '?';
	}

	// Look up a synced member by id (for rendering chips with avatar).
	const memberById = (id: string) => slackMembers.find((m) => m.memberId === id);

	// Open state for the auto-add pickers (keyed `${draft.key}:${field}`) and the
	// trigger-status combobox (keyed by draft.key). Command filters internally.
	let memberOpen = $state<Record<string, boolean>>({});
	let statusOpen = $state<Record<string, boolean>>({});

	async function saveDraft(d: Draft) {
		saving[d.key] = true;
		saveMsg[d.key] = '';
		const payload: LinearSettings = {
			teamId: d.teamId,
			creationMode: d.creationMode,
			triggerStatus: d.triggerStatus || undefined,
			nameTemplate: d.nameTemplate || undefined,
			conditionExpr: d.conditionExpr || undefined,
			autoAddMembers: d.autoAddMembers
		};
		const res = d.settingId
			? await updateLinearSettings(d.settingId, payload)
			: await createLinearSettings(payload);
		saving[d.key] = false;
		if (res.error) {
			saveMsg[d.key] = res.error;
			return;
		}
		if (res.state) applyState(res.state);
	}

	async function removeDraft(d: Draft) {
		// An unsaved draft is just dropped from the list.
		if (!d.settingId) {
			drafts = drafts.filter((x) => x.key !== d.key);
			return;
		}
		deleting[d.key] = true;
		const next = await deleteLinearSettings(d.settingId);
		deleting[d.key] = false;
		if (next) applyState(next);
	}

	async function doSync() {
		syncing = true;
		const next = await syncSettings();
		syncing = false;
		if (next) applyState(next);
	}

	async function runTest(d: Draft) {
		testing = d.key;
		// The textarea holds the event JSON (a sample's, possibly edited), so it's
		// always the source of truth; fall back to sampleId if it's somehow empty.
		const req = pastedEvent.trim()
			? { nameTemplate: d.nameTemplate, condition: d.conditionExpr, event: pastedEvent }
			: { nameTemplate: d.nameTemplate, condition: d.conditionExpr, sampleId };
		const res = await testLinearTemplate(req);
		testing = null;
		if (res) testResult[d.key] = res;
	}

	const sampleLabel = $derived(
		sampleEvents.find((s) => s.id === sampleId)?.label ?? 'Select a sample event'
	);
</script>

<!-- Multi-select combobox for Slack members: selected members show as chips; the
<!-- Auto-add members: selected members render as a stacked avatar cluster (with a
     +N overflow) that IS the trigger — clicking it opens a Command palette where
     you add/remove members (toggle via the checkmark). Empty state shows a
     "+ Add members" placeholder so there's always something to click. -->
{#snippet memberPicker(d: Draft)}
	{@const pickerKey = d.key}
	{@const selected = d.autoAddMembers}
	{@const maxAvatars = 4}
	{@const shown = selected.slice(0, maxAvatars)}
	{@const overflow = selected.length - shown.length}
	<Field.Field>
		<Field.FieldLabel>Auto-add members</Field.FieldLabel>
		<Field.FieldDescription>
			Slack members (bots and people) added to every channel this config creates.
		</Field.FieldDescription>
		<!-- Wrap in a div so the trigger button isn't a direct child of Field (whose
		     *:w-full would stretch it); the div keeps it compact/left-aligned. -->
		<div class="w-fit">
			<Popover.Root bind:open={() => memberOpen[pickerKey] ?? false, (v) => (memberOpen[pickerKey] = v)}>
				<Popover.Trigger>
					{#snippet child({ props })}
						<Button
							{...props}
							variant="outline"
							aria-label="Choose members to auto-add"
						>
						{#if selected.length === 0}
							<PlusIcon data-icon="inline-start" />
							Add members
						{:else}
							<!-- Keep the default button footprint: small avatars (and the +N
							     overflow) sized to sit inside the sm button height, so the
							     trigger never jumps as the selection changes. -->
							<Avatar.Group
								class="-space-x-1.5 *:data-[slot=avatar]:size-4 *:data-[slot=avatar-group-count]:size-4 *:data-[slot=avatar-group-count]:text-[8px]"
							>
								{#each shown as id (id)}
									{@const m = memberById(id)}
									<Avatar.Root>
										<Avatar.Image src={m?.iconUrl} alt={memberName(id)} />
										<Avatar.Fallback class="text-[8px]">{initials(memberName(id))}</Avatar.Fallback>
									</Avatar.Root>
								{/each}
								{#if overflow > 0}
									<Avatar.GroupCount>+{overflow}</Avatar.GroupCount>
								{/if}
							</Avatar.Group>
							{selected.length} selected
						{/if}
					</Button>
				{/snippet}
			</Popover.Trigger>
			<Popover.Content class="w-64 p-0" align="start">
				<Command.Root>
					<Command.Input placeholder="Search members…" />
					<Command.List>
						{#if slackMembers.length === 0}
							<Command.Empty>No members synced yet — use Sync.</Command.Empty>
						{:else}
							<Command.Empty>No members found.</Command.Empty>
							<Command.Group>
								{#each slackMembers as m (m.memberId)}
									<Command.Item value={m.name} onSelect={() => toggleAutoAdd(d, m.memberId)}>
										<CheckIcon class={cn(!selected.includes(m.memberId) && 'text-transparent')} />
										<Avatar.Root class="size-5">
											<Avatar.Image src={m.iconUrl} alt={m.name} />
											<Avatar.Fallback class="text-[9px]">{initials(m.name)}</Avatar.Fallback>
										</Avatar.Root>
										<span class="truncate">{m.name}</span>
									</Command.Item>
								{/each}
							</Command.Group>
						{/if}
					</Command.List>
				</Command.Root>
			</Popover.Content>
		</Popover.Root>
	</div>
	</Field.Field>
{/snippet}

{#if loading}
	<Card.Root>
		<Card.Header>
			<Skeleton class="h-5 w-32" />
			<Skeleton class="h-4 w-full max-w-md" />
		</Card.Header>
		<Card.Content>
			<div class="flex flex-col gap-5">
				{#each [0, 1, 2] as i (i)}
					<div class="flex flex-col gap-2">
						<Skeleton class="h-4 w-28" />
						<Skeleton class="h-9 w-full" />
					</div>
				{/each}
			</div>
		</Card.Content>
	</Card.Root>
{:else if connected}
	<div class="flex flex-col gap-4">
		<!-- Toolbar: title + refresh + new config. Icon actions carry themed tooltips. -->
		<div class="flex items-start justify-between gap-3">
			<div class="flex flex-col gap-0.5">
				<h2 class="text-base font-semibold">Channel rules</h2>
				<p class="text-muted-foreground text-sm">
					Each config opens Slack channels for the Linear teams it applies to. Templates and
					conditions use GitHub Actions expression syntax.
				</p>
			</div>
			<Tooltip.Provider delayDuration={200}>
				<div class="flex items-center gap-2">
					<Tooltip.Root>
						<Tooltip.Trigger>
							{#snippet child({ props })}
								<Button
									{...props}
									size="icon"
									variant="outline"
									onclick={doSync}
									disabled={syncing}
									aria-label="Sync teams, statuses, and Slack members"
								>
									{#if syncing}<LoaderIcon class="animate-spin" />{:else}<RefreshCwIcon />{/if}
								</Button>
							{/snippet}
						</Tooltip.Trigger>
						<Tooltip.Content>Sync teams &amp; statuses from Linear and members from Slack</Tooltip.Content>
					</Tooltip.Root>
					<!-- Add-team picker: choose a team to configure. Only teams without a
					     config yet are offered, so a team can't be configured twice. -->
					<Popover.Root bind:open={addOpen}>
						<Popover.Trigger>
							{#snippet child({ props })}
								<Button {...props} disabled={availableTeams.length === 0}>
									<PlusIcon data-icon="inline-start" />
									Add team
								</Button>
							{/snippet}
						</Popover.Trigger>
						<Popover.Content class="w-64 p-0" align="end">
							<Command.Root>
								<Command.Input placeholder="Search teams…" />
								<Command.List>
									{#if teams.length === 0}
										<Command.Empty>No teams synced yet — use Sync.</Command.Empty>
									{:else}
										<Command.Empty>No teams found.</Command.Empty>
										<Command.Group>
											{#each availableTeams as t (t.teamId)}
												<Command.Item value={t.teamName} onSelect={() => addTeam(t.teamId)}>
													<span class="truncate">{t.teamName}</span>
													{#if t.teamKey}
														<span class="text-muted-foreground ms-auto text-xs">{t.teamKey}</span>
													{/if}
												</Command.Item>
											{/each}
										</Command.Group>
									{/if}
								</Command.List>
							</Command.Root>
						</Popover.Content>
					</Popover.Root>
				</div>
			</Tooltip.Provider>
		</div>

		{#if drafts.length === 0}
			<Card.Root>
				<Card.Content class="text-muted-foreground flex flex-col items-center gap-2 py-10 text-center">
					<p class="text-sm">No teams configured yet.</p>
					<p class="text-xs">Use “Add team” above to set up channel rules for a Linear team.</p>
				</Card.Content>
			</Card.Root>
		{/if}

		{#each drafts as d (d.key)}
			{@const statusOptions = statusOptionsFor(d)}
			<Card.Root>
				<Card.Header>
					<div class="flex items-center gap-2">
						<!-- The team is the config's identity, shown as the heading. -->
						<UsersIcon class="text-muted-foreground size-4 shrink-0" />
						<Card.Title class="text-base">{teamName(d.teamId)}</Card.Title>
						{#if teamKey(d.teamId)}
							<Badge variant="secondary" class="font-mono">{teamKey(d.teamId)}</Badge>
						{/if}
						<div class="ms-auto flex items-center gap-2">
							<Tooltip.Provider delayDuration={200}>
								<Tooltip.Root>
									<Tooltip.Trigger>
										{#snippet child({ props })}
											<Button
												{...props}
												size="icon"
												variant="ghost"
												class="text-muted-foreground hover:text-destructive"
												onclick={() => removeDraft(d)}
												disabled={deleting[d.key]}
												aria-label="Remove this team's config"
											>
												{#if deleting[d.key]}<LoaderIcon class="animate-spin" />{:else}<Trash2Icon />{/if}
											</Button>
										{/snippet}
									</Tooltip.Trigger>
									<Tooltip.Content>Remove this team's config</Tooltip.Content>
								</Tooltip.Root>
							</Tooltip.Provider>
						</div>
					</div>
				</Card.Header>
				<Card.Content>
					<div class="flex flex-col gap-6">
						<!-- Rules -->
						<Field.FieldGroup>
							<Field.Field>
								<Field.FieldTitle>Channel creation</Field.FieldTitle>
								<Field.FieldDescription>
									Open a channel automatically when an issue reaches a status, or only when someone
									asks <code class="text-xs">@notifbuddy</code>.
								</Field.FieldDescription>
								<ToggleGroup.Root
									type="single"
									variant="outline"
									spacing={2}
									value={d.creationMode}
									onValueChange={(v) => {
										if (v) {
											d.creationMode = v as 'manual' | 'status';
											markEdited(d);
										}
									}}
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

							{#if d.creationMode === 'status'}
								{@const statusKey = d.key}
								<Field.Field>
									<Field.FieldLabel>Trigger status</Field.FieldLabel>
									{#if statusOptions.length > 0}
										<!-- Single-select combobox over the team's synced statuses. -->
										<Popover.Root
											bind:open={
												() => statusOpen[statusKey] ?? false, (v) => (statusOpen[statusKey] = v)
											}
										>
											<Popover.Trigger>
												{#snippet child({ props })}
													<Button
														{...props}
														variant="outline"
														role="combobox"
														aria-expanded={statusOpen[statusKey] ?? false}
														class="w-full justify-between font-normal"
													>
														{d.triggerStatus || 'Select a status'}
														<ChevronsUpDownIcon class="opacity-50" />
													</Button>
												{/snippet}
											</Popover.Trigger>
											<Popover.Content class="w-(--bits-popover-anchor-width) p-0" align="start">
												<Command.Root>
													<Command.Input placeholder="Search status…" />
													<Command.List>
														<Command.Empty>No status found.</Command.Empty>
														<Command.Group>
															{#each statusOptions as name (name)}
																<Command.Item
																	value={name}
																	onSelect={() => {
																		d.triggerStatus = name;
																		statusOpen[statusKey] = false;
																	}}
																>
																	<CheckIcon
																		class={cn(d.triggerStatus !== name && 'text-transparent')}
																	/>
																	{name}
																</Command.Item>
															{/each}
														</Command.Group>
													</Command.List>
												</Command.Root>
											</Popover.Content>
										</Popover.Root>
										<Field.FieldDescription>
											The workflow state that triggers creation.
										</Field.FieldDescription>
									{:else}
										<!-- Nothing synced yet: fall back to a plain text field. -->
										<Input bind:value={d.triggerStatus} placeholder="In Progress" />
										<Field.FieldDescription>
											No synced statuses yet. Type the exact workflow state name, or Sync above.
										</Field.FieldDescription>
									{/if}
								</Field.Field>
							{/if}

							<Field.FieldSeparator />

							<Field.Field>
								<Field.FieldLabel>Channel name</Field.FieldLabel>
								<Input
									bind:value={d.nameTemplate}
									class="font-mono text-xs"
									placeholder="tkt-${'{{'} linear.data.identifier {'}}'}"
									oninput={() => markEdited(d)}
								/>
								<Field.FieldDescription>Template for the new channel's name.</Field.FieldDescription>
							</Field.Field>

							<Field.Field>
								<Field.FieldLabel>Creation condition</Field.FieldLabel>
								<Textarea
									bind:value={d.conditionExpr}
									class="min-h-20 font-mono text-xs"
									placeholder={"linear.action == 'update' && linear.data.state.name == 'Done'"}
									oninput={() => markEdited(d)}
								/>
								<Field.FieldDescription>
									Must evaluate to true for a channel to be created. Leave empty to always create.
								</Field.FieldDescription>
							</Field.Field>

							<Field.FieldSeparator />

							{@render memberPicker(d)}
						</Field.FieldGroup>

						<!-- Test tool for this config: hidden behind a collapsible so the card
						     stays focused on editing; auto-opens on first edit. -->
						<Collapsible.Root
							bind:open={() => testOpen[d.key] ?? false, (v) => (testOpen[d.key] = v)}
							class="border-t pt-4"
						>
							<Collapsible.Trigger>
								{#snippet child({ props })}
									<button
										{...props}
										class="text-muted-foreground hover:text-foreground group/test flex w-full items-center gap-2 text-sm font-medium transition-colors"
									>
										<FlaskConicalIcon class="size-4" />
										Test against an event
										<ChevronRightIcon
											class="size-4 transition-transform group-data-[state=open]/test:rotate-90"
										/>
									</button>
								{/snippet}
							</Collapsible.Trigger>
							<Collapsible.Content class="flex flex-col gap-3 pt-3">
								<p class="text-muted-foreground text-xs">
									Preview this config's channel name and condition against a sample or pasted event.
								</p>
								<Select.Root type="single" value={sampleId} onValueChange={selectSample}>
								<Select.Trigger class="w-full">{sampleLabel}</Select.Trigger>
								<Select.Content>
									<Select.Group>
										{#each sampleEvents as s (s.id)}
											<Select.Item value={s.id} label={s.label}>{s.label}</Select.Item>
										{/each}
									</Select.Group>
								</Select.Content>
							</Select.Root>
							<Textarea
								bind:value={pastedEvent}
								class="min-h-32 font-mono text-xs"
								placeholder={'Paste raw event JSON:\n{ "event_type": "linear", "linear": { … } }'}
							/>
							<Button
								variant="outline"
								class="w-fit"
								onclick={() => runTest(d)}
								disabled={testing === d.key}
							>
								{#if testing === d.key}<LoaderIcon class="animate-spin" />{:else}<PlayIcon />{/if}
								Run test
							</Button>

							{#if testResult[d.key]}
								{@const r = testResult[d.key]}
								<div class="flex flex-col gap-2 border-t pt-3 text-sm">
									{#if r.error}
										<p class="text-destructive font-mono text-xs">{r.error}</p>
									{:else}
										<div class="flex items-center gap-2">
											<span class="text-muted-foreground text-xs">Channel name</span>
											<code class="font-mono text-xs">{r.name || '(empty)'}</code>
										</div>
										<div class="flex items-center gap-2">
											<span class="text-muted-foreground text-xs">Condition</span>
											{#if r.conditionResult}
												<Badge variant="secondary" class="gap-1"><CheckIcon class="size-3" /> true</Badge>
											{:else}
												<Badge variant="outline" class="gap-1"><XIcon class="size-3" /> false</Badge>
											{/if}
										</div>
									{/if}
								</div>
							{/if}
						</Collapsible.Content>
					</Collapsible.Root>
					</div>
				</Card.Content>
				<Card.Footer class="gap-3">
					<Button onclick={() => saveDraft(d)} disabled={saving[d.key]}>
						{#if saving[d.key]}<LoaderIcon class="animate-spin" />{:else}<SaveIcon />{/if}
						Save
					</Button>
					{#if saveMsg[d.key]}
						<span class="text-destructive text-sm">{saveMsg[d.key]}</span>
					{/if}
				</Card.Footer>
			</Card.Root>
		{/each}
	</div>
{:else}
	<!-- Linear isn't connected at the workspace level, so there are no channel
	     rules to configure. Point the user to the integrations page to connect. -->
	<Card.Root>
		<Card.Content class="flex flex-col items-center gap-4 py-12 text-center">
			<!-- Both integrations are required: Linear (source) + Slack (destination).
			     [experimental] Each shows a check badge when connected, dims when not. -->
			<div class="text-muted-foreground flex items-center gap-2">
				<div class="relative">
					<div
						class="bg-muted flex size-12 items-center justify-center rounded-full transition-opacity"
						class:opacity-40={!linearConnected}
					>
						<SiLinear class="size-6" />
					</div>
					{#if linearConnected}
						<span
							class="bg-primary text-primary-foreground border-card absolute -end-0.5 -bottom-0.5 flex size-5 items-center justify-center rounded-full border-2"
						>
							<CheckIcon class="size-3" />
						</span>
					{/if}
				</div>
				<PlusIcon class="size-4" />
				<div class="relative">
					<div
						class="bg-muted flex size-12 items-center justify-center rounded-full transition-opacity"
						class:opacity-40={!slackConnected}
					>
						<SlackIcon class="size-6" />
					</div>
					{#if slackConnected}
						<span
							class="bg-primary text-primary-foreground border-card absolute -end-0.5 -bottom-0.5 flex size-5 items-center justify-center rounded-full border-2"
						>
							<CheckIcon class="size-3" />
						</span>
					{/if}
				</div>
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
