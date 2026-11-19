<script lang="ts">
	import CategoryRow from './CategoryRow.svelte';
	import { groupSlug, type GroupColor } from '$lib/utils/budgetColors';
	import type { CategoryBudget } from '$lib/types/budget';

	interface Props {
		groupName: string;
		color: GroupColor;
		categories: CategoryBudget[];
		displayMode: 'remaining' | 'spent';
		highlighted?: boolean;
		onToggleDisplayMode: () => void;
		onCoverClick: (id: string) => void;
		onEditClick: (id: string) => void;
		onUpdatePlanned: (id: string, amount: number) => Promise<void> | void;
		onFilterTransactions?: (id: string) => void;
		onAddCategoryInGroup: (groupName: string) => void;
	}

	let {
		groupName,
		color,
		categories,
		displayMode,
		highlighted = false,
		onToggleDisplayMode,
		onCoverClick,
		onEditClick,
		onUpdatePlanned,
		onFilterTransactions,
		onAddCategoryInGroup
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
	class="rounded-2xl border bg-white dark:bg-slate-900 shadow-xs overflow-hidden transition-shadow {highlighted
		? `border-sky-400 dark:border-sky-500 ring-2 ${color.ringClass}`
		: 'border-slate-200 dark:border-slate-800'}"
>
	<!-- Group Header -->
	<div class="px-5 pt-4 pb-3 border-b border-slate-100 dark:border-slate-800/80 grid grid-cols-[minmax(0,1fr)_96px_96px] sm:grid-cols-[minmax(0,1fr)_116px_116px] items-end gap-3">
		<div class="flex items-center gap-2 min-w-0">
			<span class="w-2.5 h-2.5 rounded-full shrink-0 {color.dotClass}" aria-hidden="true"></span>
			<h3 class="text-base font-bold text-slate-900 dark:text-white truncate">{groupName}</h3>
		</div>

		<div class="text-right">
			<div class="text-sm font-mono font-bold text-slate-900 dark:text-white">
				{fmt(groupPlannedTotal)}
			</div>
			<div class="text-[11px] font-medium text-slate-500 dark:text-slate-400">Planned</div>
		</div>

		<div class="text-right">
			<div
				class="text-sm font-mono font-bold {displayMode === 'remaining' && groupRemainingTotal < 0
					? 'text-rose-600 dark:text-rose-400'
					: 'text-slate-900 dark:text-white'}"
			>
				{fmt(displayMode === 'spent' ? groupSpentTotal : groupRemainingTotal)}
			</div>
			<button
				type="button"
				onclick={onToggleDisplayMode}
				class="inline-flex items-center justify-end gap-0.5 text-[11px] font-medium text-sky-600 dark:text-sky-400 hover:underline"
				title="Switch between Remaining and Spent"
			>
				<span>{displayMode === 'spent' ? 'Spent' : 'Remaining'}</span>
				<span aria-hidden="true">&#9662;</span>
			</button>
		</div>
	</div>

	<!-- Categories -->
	<div class="divide-y divide-slate-100 dark:divide-slate-800/80">
		{#each categories as cat (cat.id)}
			<CategoryRow
				category={cat}
				{displayMode}
				embedded={true}
				{onCoverClick}
				{onEditClick}
				{onUpdatePlanned}
				{onFilterTransactions}
			/>
		{/each}
	</div>

	<!-- Footer -->
	<div class="px-5 py-2.5 bg-slate-50/60 dark:bg-slate-900/60 border-t border-slate-100 dark:border-slate-800/80">
		<button
			type="button"
			onclick={() => onAddCategoryInGroup(groupName)}
			class="text-xs font-semibold text-sky-600 hover:text-sky-700 dark:text-sky-400 dark:hover:text-sky-300 transition-colors"
		>
			+ Add Item
		</button>
	</div>
</section>
