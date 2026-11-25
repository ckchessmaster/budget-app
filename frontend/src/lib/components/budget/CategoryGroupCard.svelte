<script lang="ts">
	import CategoryRow from './CategoryRow.svelte';
	import { groupSlug, type GroupColor } from '$lib/utils/budgetColors';
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
		onDropTransaction
	}: Props = $props();

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
	class="rounded-2xl border bg-surface shadow-xs overflow-hidden transition-shadow {highlighted
		? `border-accent-pop ring-2 ${color.ringClass}`
		: 'border-border'}"
>
	<!-- Group Header -->
	<div class="px-5 pt-4 pb-3 border-b border-border grid grid-cols-[minmax(0,1fr)_88px_96px] sm:grid-cols-[minmax(0,1fr)_104px_116px] items-end gap-3">
		<div class="flex items-center gap-2 min-w-0">
			<span class="w-2.5 h-2.5 rounded-full shrink-0 {color.dotClass}" aria-hidden="true"></span>
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
	<div class="px-5 py-2.5 bg-subtle border-t border-border">
		<button
			type="button"
			onclick={() => onAddCategoryInGroup(groupName)}
			class="text-xs font-semibold text-accent-pop hover:underline transition-colors"
		>
			+ Add Item
		</button>
	</div>
</section>
