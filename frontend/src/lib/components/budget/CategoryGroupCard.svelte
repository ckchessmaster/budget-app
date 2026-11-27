<script lang="ts">
	import CategoryRow from './CategoryRow.svelte';
	import { GROUP_COLOR_PALETTE, groupSlug, type GroupColor } from '$lib/utils/budgetColors';
	import type { CategoryBudget, InlineActivityItem } from '$lib/types/budget';
	import type { AccountItem } from '$lib/types/transaction';

	interface Props {
		groupName: string;
		color: GroupColor;
		categories: CategoryBudget[];
		accountById?: Map<string, AccountItem>;
		displayMode: 'remaining' | 'spent';
		highlighted?: boolean;
		transactionsByCategory?: Map<string, InlineActivityItem[]>;
		draggingCategoryId?: string | null;
		draggingGroupName?: string | null;
		draggingTxId?: string | null;
		organizeMode?: boolean;
		isCollapsed?: boolean;
		isFirstGroup?: boolean;
		isLastGroup?: boolean;
		availableGroups?: string[];
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
		onGroupDragStart?: (groupName: string) => void;
		onGroupDragEnd?: () => void;
		onReorderGroup?: (
			draggedGroupName: string,
			targetGroupName: string,
			position: 'before' | 'after'
		) => Promise<void> | void;
		onMoveGroupStep?: (groupName: string, direction: 'up' | 'down') => Promise<void> | void;
		onToggleCollapse?: (groupName: string) => void;
		onSelectTransaction?: (txId: string) => void;
	}

	let {
		groupName,
		color,
		categories,
		accountById,
		displayMode,
		highlighted = false,
		transactionsByCategory,
		draggingCategoryId = null,
		draggingGroupName = null,
		draggingTxId = null,
		organizeMode = false,
		isCollapsed = false,
		isFirstGroup = false,
		isLastGroup = false,
		availableGroups = [],
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
		onGroupDragStart,
		onGroupDragEnd,
		onReorderGroup,
		onMoveGroupStep,
		onToggleCollapse,
		onSelectTransaction
	}: Props = $props();

	let sectionEl = $state<HTMLElement | null>(null);
	let upBtnEl = $state<HTMLButtonElement | null>(null);
	let downBtnEl = $state<HTMLButtonElement | null>(null);
	let colorPickerOpen = $state(false);
	let isEmptyDropOver = $state(false);
	let groupDropPosition = $state<'before' | 'after' | null>(null);

	let prevDraggingGroupName: string | null = null;
	$effect(() => {
		const current = draggingGroupName;
		if (prevDraggingGroupName !== null && current === null) {
			groupDropPosition = null;
		}
		prevDraggingGroupName = current;
	});

	let prevDraggingCategoryId: string | null = null;
	$effect(() => {
		const current = draggingCategoryId;
		if (prevDraggingCategoryId !== null && current === null) {
			isEmptyDropOver = false;
		}
		prevDraggingCategoryId = current;
	});

	function isGroupDragEvent(e: DragEvent): boolean {
		if (draggingGroupName !== null) return true;
		const types = e.dataTransfer?.types;
		if (!types) return false;
		return Array.from(types).includes('application/x-budget-group-name');
	}

	async function handleMoveGroup(direction: 'up' | 'down') {
		if (!onMoveGroupStep) return;
		await onMoveGroupStep(groupName, direction);
		queueMicrotask(() => {
			if (direction === 'up' && isFirstGroup && downBtnEl && !downBtnEl.disabled) {
				downBtnEl.focus({ preventScroll: true });
			} else if (direction === 'down' && isLastGroup && upBtnEl && !upBtnEl.disabled) {
				upBtnEl.focus({ preventScroll: true });
			}
		});
	}

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

	function handleKeydown(e: KeyboardEvent) {
		if (colorPickerOpen && e.key === 'Escape') {
			e.preventDefault();
			colorPickerOpen = false;
		}
	}
</script>

<svelte:window onkeydown={handleKeydown} />

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<section
	bind:this={sectionEl}
	id={groupSlug(groupName)}
	aria-label={groupName}
	data-testid="category-group-card"
	style="border-left: 3px solid {color.stroke};"
	ondragover={(e) => {
		if (!onReorderGroup || !isGroupDragEvent(e)) return;
		if (draggingGroupName === groupName) return;
		e.preventDefault();
		if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
		const rect = e.currentTarget.getBoundingClientRect();
		const midY = rect.top + rect.height / 2;
		groupDropPosition = e.clientY < midY ? 'before' : 'after';
	}}
	ondragleave={(e) => {
		const related = e.relatedTarget as Node | null;
		if (related && e.currentTarget.contains(related)) return;
		groupDropPosition = null;
	}}
	ondrop={async (e) => {
		const pos = groupDropPosition ?? 'before';
		groupDropPosition = null;
		if (!onReorderGroup) return;
		const draggedGroup =
			e.dataTransfer?.getData('application/x-budget-group-name') || draggingGroupName;
		if (draggedGroup) {
			e.preventDefault();
			if (draggedGroup !== groupName) {
				await onReorderGroup(draggedGroup, groupName, pos);
			}
		}
	}}
	class="relative rounded-2xl border bg-surface shadow-xs transition-[opacity,box-shadow,background-color] duration-150 {highlighted
		? `border-accent-pop ${color.ringClass}`
		: 'border-border'} {groupDropPosition !== null ? 'shadow-md bg-accent-tint/20' : ''} {draggingGroupName === groupName
		? 'opacity-50'
		: ''}"
>
	{#if groupDropPosition !== null}
		<div
			aria-hidden="true"
			data-testid="group-drop-indicator"
			class="pointer-events-none absolute inset-x-4 h-[3px] rounded-full bg-accent-pop/80 z-20 {groupDropPosition === 'before'
				? '-top-2.5'
				: '-bottom-2.5'}"
		></div>
	{/if}

	<!-- Group Header -->
	<div
		style="background-color: {groupDropPosition !== null ? 'var(--accent-tint)' : `${color.stroke}14`};"
		class="{organizeMode && isCollapsed ? 'rounded-2xl' : 'rounded-t-2xl border-b border-border'} px-3.5 sm:px-5 pt-4 pb-3 transition-colors duration-150 grid grid-cols-[minmax(0,1fr)_auto] sm:grid-cols-[minmax(0,1fr)_104px_116px] items-center sm:items-end gap-2 sm:gap-3"
	>
		<div class="relative flex items-center gap-2 min-w-0">
			{#if onReorderGroup}
				<button
					type="button"
					data-testid="group-drag-handle"
					draggable={true}
					aria-label={`Reorder ${groupName} category`}
					title="Drag to reorder category (or use Up/Down arrow keys)"
					ondragstart={(e) => {
						e.stopPropagation();
						e.dataTransfer?.setData('application/x-budget-group-name', groupName);
						if (e.dataTransfer) {
							e.dataTransfer.effectAllowed = 'move';
							if (sectionEl && typeof e.dataTransfer.setDragImage === 'function') {
								e.dataTransfer.setDragImage(sectionEl, 24, 24);
							}
						}
						onGroupDragStart?.(groupName);
					}}
					ondragend={() => {
						groupDropPosition = null;
						onGroupDragEnd?.();
					}}
					onclick={(e) => e.stopPropagation()}
					onkeydown={(e) => {
						if (e.key === 'ArrowUp' && onMoveGroupStep) {
							e.preventDefault();
							e.stopPropagation();
							void handleMoveGroup('up');
						} else if (e.key === 'ArrowDown' && onMoveGroupStep) {
							e.preventDefault();
							e.stopPropagation();
							void handleMoveGroup('down');
						}
					}}
					class="hidden sm:inline-flex p-1 -ml-1 rounded text-ink-muted hover:text-ink-primary hover:bg-surface/80 cursor-grab active:cursor-grabbing shrink-0 touch-none select-none [-webkit-user-select:none]"
				>
					<svg class="w-3.5 h-3.5" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
						<circle cx="5.5" cy="3.5" r="1.2" />
						<circle cx="10.5" cy="3.5" r="1.2" />
						<circle cx="5.5" cy="8" r="1.2" />
						<circle cx="10.5" cy="8" r="1.2" />
						<circle cx="5.5" cy="12.5" r="1.2" />
						<circle cx="10.5" cy="12.5" r="1.2" />
					</svg>
				</button>
			{/if}
			{#if onChangeGroupColor && !organizeMode}
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
			{#if organizeMode && isCollapsed}
				<span class="text-[11px] font-medium text-ink-muted shrink-0">
					{categories.length} {categories.length === 1 ? 'item' : 'items'}
				</span>
			{/if}
			{#if onEditGroup && !organizeMode}
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

		{#if organizeMode}
			<div
				data-testid="group-organize-controls"
				class="sm:col-span-2 flex items-center justify-end gap-1.5 shrink-0"
			>
				{#if onToggleCollapse}
					<button
						type="button"
						data-testid="toggle-group-collapse-btn"
						aria-expanded={!isCollapsed}
						aria-label={`${isCollapsed ? 'Expand' : 'Collapse'} ${groupName} items`}
						onclick={() => onToggleCollapse(groupName)}
						class="h-8 px-2.5 rounded-lg border border-border bg-surface text-xs font-medium text-ink-secondary hover:bg-surface-hover hover:text-ink-primary transition-colors"
					>
						{isCollapsed ? 'Expand' : 'Collapse'}
					</button>
				{/if}
				{#if onMoveGroupStep}
					<button
						bind:this={upBtnEl}
						type="button"
						data-testid="move-group-up-btn"
						aria-label={`Move ${groupName} up`}
						disabled={isFirstGroup}
						onclick={() => void handleMoveGroup('up')}
						class="h-8 w-8 inline-flex items-center justify-center rounded-lg border border-border bg-surface text-ink-primary hover:bg-surface-hover disabled:opacity-35 disabled:cursor-not-allowed transition-colors"
					>
						<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
							<path d="m18 15-6-6-6 6" />
						</svg>
					</button>
					<button
						bind:this={downBtnEl}
						type="button"
						data-testid="move-group-down-btn"
						aria-label={`Move ${groupName} down`}
						disabled={isLastGroup}
						onclick={() => void handleMoveGroup('down')}
						class="h-8 w-8 inline-flex items-center justify-center rounded-lg border border-border bg-surface text-ink-primary hover:bg-surface-hover disabled:opacity-35 disabled:cursor-not-allowed transition-colors"
					>
						<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
							<path d="m6 9 6 6 6-6" />
						</svg>
					</button>
				{/if}
			</div>
		{:else}
			<div class="hidden sm:block text-right">
				<div class="text-xs font-mono font-medium text-ink-muted">
					{fmt(groupPlannedTotal)}
				</div>
				<div class="text-[11px] font-medium text-ink-muted">Planned</div>
			</div>

			<div class="flex sm:block items-center gap-3 text-right">
				<div class="sm:hidden text-right">
					<div class="text-xs font-mono font-medium text-ink-muted">
						{fmt(groupPlannedTotal)}
					</div>
					<div class="text-[11px] font-medium text-ink-muted">Planned</div>
				</div>
				<div>
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
		{/if}
	</div>

	<!-- Items -->
	{#if !(organizeMode && isCollapsed)}
		{#if categories.length === 0}
			<!-- svelte-ignore a11y_no_static_element_interactions -->
			<div
				data-testid="empty-group-dropzone"
				ondragover={(e) => {
					if (!onMoveCategoryToGroup || isGroupDragEvent(e)) return;
					const hasCatType =
						draggingCategoryId !== null ||
						Boolean(e.dataTransfer?.types && Array.from(e.dataTransfer.types).includes('application/x-budget-category-id'));
					if (!hasCatType) return;
					e.preventDefault();
					if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
					isEmptyDropOver = true;
				}}
				ondragleave={(e) => {
					const related = e.relatedTarget as Node | null;
					if (related && e.currentTarget.contains(related)) return;
					isEmptyDropOver = false;
				}}
				ondrop={async (e) => {
					isEmptyDropOver = false;
					if (!onMoveCategoryToGroup || isGroupDragEvent(e)) return;
					const draggedCatId =
						e.dataTransfer?.getData('application/x-budget-category-id') || draggingCategoryId;
					if (draggedCatId) {
						e.preventDefault();
						await onMoveCategoryToGroup(draggedCatId, groupName);
					}
				}}
				class="px-4 py-6 text-center text-xs text-ink-muted transition-colors duration-150 {isEmptyDropOver
					? 'bg-accent-tint text-ink-primary'
					: ''}"
			>
				No items yet. Click "+ Add Item" below or drag an item here.
			</div>
		{:else}
			<div class="divide-y divide-border">
				{#each categories as cat, idx (cat.id)}
					<CategoryRow
						category={cat}
						{displayMode}
						embedded={true}
						groupColor={color}
						heldInAccountName={cat.heldInAccountId ? (accountById?.get(cat.heldInAccountId)?.name ?? null) : null}
						transactions={transactionsByCategory?.get(cat.id) ?? []}
						{draggingCategoryId}
						{draggingGroupName}
						{draggingTxId}
						{organizeMode}
						isFirstInGroup={idx === 0}
						isLastInGroup={idx === categories.length - 1}
						{availableGroups}
						{selectedTransactionId}
						{onCoverClick}
						{onEditClick}
						{onUpdatePlanned}
						{onDropTransaction}
						{onCategoryDragStart}
						{onCategoryDragEnd}
						{onReorderCategory}
						{onMoveCategoryStep}
						{onMoveCategoryToGroup}
						{onSelectTransaction}
					/>
				{/each}
			</div>
		{/if}

		{#if !organizeMode}
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
		{/if}
	{/if}
</section>
