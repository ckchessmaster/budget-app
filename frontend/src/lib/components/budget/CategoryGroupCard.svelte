<script lang="ts">
	import CategoryRow from './CategoryRow.svelte';
	import { GROUP_COLOR_PALETTE, groupSlug, type GroupColor } from '$lib/utils/budgetColors';
	import type { CategoryBudget, InlineActivityItem } from '$lib/types/budget';

	interface Props {
		groupName: string;
		color: GroupColor;
		categories: CategoryBudget[];
		displayMode: 'remaining' | 'spent';
		highlighted?: boolean;
		transactionsByCategory?: Map<string, InlineActivityItem[]>;
		onToggleDisplayMode: () => void;
		onCoverClick: (id: string) => void;
		onEditClick: (id: string) => void;
		onUpdatePlanned: (id: string, amount: number) => Promise<void> | void;
		onFilterTransactions?: (id: string) => void;
		onAddCategoryInGroup: (groupName: string) => void;
		onDropTransaction?: (txId: string, categoryId: string) => Promise<void> | void;
		onChangeGroupColor?: (groupName: string, colorIndex: number) => void;
		onDeleteGroup?: (groupName: string) => Promise<void> | void;
	}

	let {
		groupName,
		color,
		categories,
		displayMode,
		highlighted = false,
		transactionsByCategory,
		onToggleDisplayMode,
		onCoverClick,
		onEditClick,
		onUpdatePlanned,
		onFilterTransactions,
		onAddCategoryInGroup,
		onDropTransaction,
		onChangeGroupColor,
		onDeleteGroup
	}: Props = $props();

	let colorPickerOpen = $state(false);
	let confirmDeleteGroup = $state(false);
	let deletingGroup = $state(false);

	let groupPlannedTotal = $derived(categories.reduce((sum, c) => sum + c.effectiveTarget, 0));
	let groupSpentTotal = $derived(
		categories.reduce((sum, c) => sum + c.spentCleared + c.spentPending, 0)
	);
	let groupRemainingTotal = $derived(categories.reduce((sum, c) => sum + c.remaining, 0));

	function fmt(n: number): string {
		const abs = Math.abs(n).toLocaleString('en-US', {
			minimumFractionDigits: 2,
			maximumFractionDigits: 2
		});
		return n < 0 ? `-$${abs}` : `$${abs}`;
	}
</script>

<section
	id={groupSlug(groupName)}
	aria-label={groupName}
	data-testid="category-group-card"
	style="border-left: 3px solid {color.stroke};"
	class="rounded-2xl border bg-surface shadow-xs overflow-hidden transition-shadow {highlighted
		? `border-accent-pop ring-2 ${color.ringClass}`
		: 'border-border'}"
>
	<!-- Group Header -->
	<div
		style="background-color: {color.stroke}14;"
		class="px-5 pt-4 pb-3 border-b border-border grid grid-cols-[minmax(0,1fr)_88px_96px] sm:grid-cols-[minmax(0,1fr)_104px_116px] items-end gap-3"
	>
		<div class="relative flex items-center gap-2 min-w-0">
			{#if onChangeGroupColor}
				<button
					type="button"
					onclick={() => (colorPickerOpen = !colorPickerOpen)}
					title="Change group color"
					aria-label={`Change color for ${groupName}`}
					class="w-3 h-3 rounded-full shrink-0 {color.dotClass} ring-2 ring-offset-1 ring-offset-surface ring-transparent hover:ring-ink-muted/40 transition-all"
				></button>
				{#if colorPickerOpen}
					<div
						class="absolute left-0 top-7 z-20 flex items-center gap-1.5 rounded-xl border border-border bg-surface p-2 shadow-lg"
						role="dialog"
						aria-label={`Color picker for ${groupName}`}
					>
						{#each GROUP_COLOR_PALETTE as swatch, idx (swatch.name)}
							<button
								type="button"
								aria-label={swatch.name}
								title={swatch.name}
								onclick={() => {
									onChangeGroupColor(groupName, idx);
									colorPickerOpen = false;
								}}
								class="w-4 h-4 rounded-full {swatch.dotClass} hover:scale-110 transition-transform {swatch.stroke === color.stroke ? 'ring-2 ring-ink-primary ring-offset-1 ring-offset-surface' : ''}"
							></button>
						{/each}
					</div>
				{/if}
			{:else}
				<span class="w-2.5 h-2.5 rounded-full shrink-0 {color.dotClass}" aria-hidden="true"></span>
			{/if}
			<h3 class="text-base font-bold text-ink-primary truncate">{groupName}</h3>
		</div>

		<div class="text-right">
			<div class="text-xs font-mono font-medium text-ink-muted">
				{fmt(groupPlannedTotal)}
			</div>
			<div class="text-[11px] font-medium text-ink-muted">Planned</div>
		</div>

		<div class="text-right">
			<div
				class="text-sm sm:text-base font-mono font-bold {displayMode === 'remaining' && groupRemainingTotal < 0
					? 'text-status-danger'
					: 'text-ink-primary'}"
			>
				{fmt(displayMode === 'spent' ? groupSpentTotal : groupRemainingTotal)}
			</div>
			<button
				type="button"
				onclick={onToggleDisplayMode}
				class="inline-flex items-center justify-end gap-0.5 text-[11px] font-medium text-accent-pop hover:underline"
				title="Switch between Remaining and Spent"
			>
				<span>{displayMode === 'spent' ? 'Spent' : 'Remaining'}</span>
				<span aria-hidden="true">&#9662;</span>
			</button>
		</div>
	</div>

	<!-- Categories -->
	<div class="divide-y divide-border">
		{#each categories as cat (cat.id)}
			<CategoryRow
				category={cat}
				{displayMode}
				embedded={true}
				transactions={transactionsByCategory?.get(cat.id) ?? []}
				{onCoverClick}
				{onEditClick}
				{onUpdatePlanned}
				{onFilterTransactions}
				{onDropTransaction}
			/>
		{/each}
	</div>

	<!-- Footer -->
	<div class="px-5 py-2.5 bg-subtle border-t border-border flex items-center justify-between gap-2">
		<button
			type="button"
			onclick={() => onAddCategoryInGroup(groupName)}
			class="text-xs font-semibold text-accent-pop hover:underline transition-colors"
		>
			+ Add Item
		</button>

		{#if onDeleteGroup}
			{#if confirmDeleteGroup}
				<div class="flex items-center gap-2">
					<span class="text-[11px] text-status-danger">Delete group?</span>
					<button
						type="button"
						disabled={deletingGroup}
						onclick={async () => {
							deletingGroup = true;
							try {
								await onDeleteGroup(groupName);
							} finally {
								deletingGroup = false;
								confirmDeleteGroup = false;
							}
						}}
						class="text-[11px] font-semibold text-status-danger hover:underline disabled:opacity-50"
					>
						{deletingGroup ? 'Deleting...' : 'Confirm'}
					</button>
					<button
						type="button"
						disabled={deletingGroup}
						onclick={() => (confirmDeleteGroup = false)}
						class="text-[11px] text-ink-secondary hover:text-ink-primary"
					>
						Cancel
					</button>
				</div>
			{:else}
				<button
					type="button"
					onclick={() => (confirmDeleteGroup = true)}
					class="text-[11px] text-ink-muted hover:text-status-danger transition-colors"
				>
					Delete Group
				</button>
			{/if}
		{/if}
	</div>
</section>
