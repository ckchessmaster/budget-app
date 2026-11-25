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
		draggingCategoryId?: string | null;
		selectedTransactionId?: string | null;
		onToggleDisplayMode: () => void;
		onCoverClick: (id: string) => void;
		onEditClick: (id: string) => void;
		onEditGroup?: (groupName: string) => void;
		onUpdatePlanned: (id: string, amount: number) => Promise<void> | void;
		onAddCategoryInGroup: (groupName: string) => void;
		onDropTransaction?: (txId: string, categoryId: string) => Promise<void> | void;
		onChangeGroupColor?: (groupName: string, colorIndex: number) => void;
		onCategoryDragStart?: (categoryId: string) => void;
		onCategoryDragEnd?: () => void;
		onReorderCategory?: (
			draggedId: string,
			targetId: string,
			position: 'before' | 'after'
		) => Promise<void> | void;
		onMoveCategoryToGroup?: (draggedId: string, targetGroupName: string) => Promise<void> | void;
		onMoveCategoryStep?: (categoryId: string, direction: 'up' | 'down') => Promise<void> | void;
		onSelectTransaction?: (txId: string) => void;
	}

	let {
		groupName,
		color,
		categories,
		displayMode,
		highlighted = false,
		transactionsByCategory,
		draggingCategoryId = null,
		selectedTransactionId = null,
		onToggleDisplayMode,
		onCoverClick,
		onEditClick,
		onEditGroup,
		onUpdatePlanned,
		onAddCategoryInGroup,
		onDropTransaction,
		onChangeGroupColor,
		onCategoryDragStart,
		onCategoryDragEnd,
		onReorderCategory,
		onMoveCategoryToGroup,
		onMoveCategoryStep,
		onSelectTransaction
	}: Props = $props();

	let colorPickerOpen = $state(false);
	let isEmptyDropOver = $state(false);

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
	class="rounded-2xl border bg-surface shadow-xs transition-shadow {highlighted
		? `border-accent-pop ring-2 ${color.ringClass}`
		: 'border-border'}"
>
	<!-- Group Header -->
	<div
		style="background-color: {color.stroke}14;"
		class="rounded-t-2xl px-3.5 sm:px-5 pt-4 pb-3 border-b border-border grid grid-cols-[minmax(0,1fr)_76px_88px] sm:grid-cols-[minmax(0,1fr)_104px_116px] items-end gap-2 sm:gap-3"
	>
		<div class="relative flex items-center gap-2 min-w-0">
			{#if onChangeGroupColor}
				<button
					type="button"
					onclick={() => (colorPickerOpen = !colorPickerOpen)}
					title="Change category color"
					aria-label={`Change color for ${groupName}`}
					class="p-1 -m-1 rounded-full shrink-0 hover:bg-surface/70 transition-colors"
				>
					<span class="block w-3.5 h-3.5 rounded-full {color.dotClass} ring-2 ring-offset-1 ring-offset-surface ring-transparent"></span>
				</button>
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
								class="w-5 h-5 rounded-full {swatch.dotClass} hover:scale-110 transition-transform {swatch.stroke === color.stroke ? 'ring-2 ring-ink-primary ring-offset-1 ring-offset-surface' : ''}"
							></button>
						{/each}
					</div>
				{/if}
			{:else}
				<span class="w-3 h-3 rounded-full shrink-0 {color.dotClass}" aria-hidden="true"></span>
			{/if}
			<h3 class="text-base font-bold text-ink-primary truncate">{groupName}</h3>
			{#if onEditGroup}
				<button
					type="button"
					data-testid="edit-group-btn"
					aria-label={`Edit ${groupName}`}
					onclick={() => onEditGroup(groupName)}
					class="px-2.5 py-1 rounded-lg text-xs font-medium text-ink-secondary hover:text-ink-primary hover:bg-surface/80 transition-colors shrink-0"
				>
					Edit
				</button>
			{/if}
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

	<!-- Items -->
	{#if categories.length === 0}
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<div
			data-testid="empty-group-dropzone"
			ondragover={(e) => {
				if (!onMoveCategoryToGroup) return;
				const hasCatType =
					draggingCategoryId !== null ||
					Boolean(e.dataTransfer?.types && Array.from(e.dataTransfer.types).includes('application/x-budget-category-id'));
				if (!hasCatType) return;
				e.preventDefault();
				if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
				isEmptyDropOver = true;
			}}
			ondragleave={() => {
				isEmptyDropOver = false;
			}}
			ondrop={async (e) => {
				isEmptyDropOver = false;
				if (!onMoveCategoryToGroup) return;
				const draggedCatId =
					e.dataTransfer?.getData('application/x-budget-category-id') || draggingCategoryId;
				if (draggedCatId) {
					e.preventDefault();
					await onMoveCategoryToGroup(draggedCatId, groupName);
				}
			}}
			class="px-4 py-6 text-center text-xs text-ink-muted transition-colors {isEmptyDropOver
				? 'ring-2 ring-inset ring-accent-pop bg-accent-tint/30 text-ink-primary'
				: ''}"
		>
			No items yet. Click "+ Add Item" below or drag an item here.
		</div>
	{:else}
		<div class="divide-y divide-border">
			{#each categories as cat (cat.id)}
				<CategoryRow
					category={cat}
					{displayMode}
					embedded={true}
					transactions={transactionsByCategory?.get(cat.id) ?? []}
					{draggingCategoryId}
					{selectedTransactionId}
					{onCoverClick}
					{onEditClick}
					{onUpdatePlanned}
					{onDropTransaction}
					{onCategoryDragStart}
					{onCategoryDragEnd}
					{onReorderCategory}
					{onMoveCategoryStep}
					{onSelectTransaction}
				/>
			{/each}
		</div>
	{/if}

	<!-- Footer -->
	<div class="rounded-b-2xl px-3.5 sm:px-5 py-2 bg-subtle border-t border-border flex items-center justify-between gap-2">
		<button
			type="button"
			onclick={() => onAddCategoryInGroup(groupName)}
			class="px-2 py-1.5 -ml-2 rounded-lg text-xs font-semibold text-accent-pop hover:bg-surface-hover transition-colors"
		>
			+ Add Item
		</button>
	</div>
</section>
