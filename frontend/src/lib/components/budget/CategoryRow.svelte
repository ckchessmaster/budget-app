<script lang="ts">
	import type { InlineActivityItem, RolloverMode } from '$lib/types/budget';
	import type { GroupColor } from '$lib/utils/budgetColors';
	import InlineActivityDrawer from './InlineActivityDrawer.svelte';

	export interface CategoryRowItem {
		id: string;
		name: string;
		groupName?: string;
		rolloverMode?: RolloverMode;
		heldInAccountId?: string | null;
		targetAmount?: number | null;
		targetDate?: string | null;
		carryoverBalance?: number;
		kind?: 'fixed' | 'variable';
		originalPlanned: number;
		effectiveTarget: number;
		spentCleared: number;
		spentPending: number;
		remaining: number;
		pacing?: {
			monthlyDue: number;
			monthsRemaining: number;
			isMet: boolean;
		};
	}

	interface Props {
		category: CategoryRowItem;
		displayMode?: 'remaining' | 'spent';
		embedded?: boolean;
		groupColor?: GroupColor;
		heldInAccountName?: string | null;
		transactions?: InlineActivityItem[];
		draggingCategoryId?: string | null;
		draggingGroupName?: string | null;
		draggingTxId?: string | null;
		organizeMode?: boolean;
		isFirstInGroup?: boolean;
		isLastInGroup?: boolean;
		availableGroups?: string[];
		selectedTransactionId?: string | null;
		onCoverClick: (categoryId: string) => void;
		onEditClick?: (categoryId: string) => void;
		onUpdatePlanned?: (categoryId: string, newPlanned: number) => Promise<void> | void;
		onDropTransaction?: (txId: string, categoryId: string) => Promise<void> | void;
		onCategoryDragStart?: (categoryId: string) => void;
		onCategoryDragEnd?: () => void;
		onReorderCategory?: (
			draggedId: string,
			targetId: string,
			position: 'before' | 'after'
		) => Promise<void> | void;
		onMoveCategoryStep?: (categoryId: string, direction: 'up' | 'down') => Promise<void> | void;
		onMoveCategoryToGroup?: (categoryId: string, targetGroupName: string) => Promise<void> | void;
		onSelectTransaction?: (txId: string) => void;
	}

	let {
		category,
		displayMode = 'remaining',
		embedded = false,
		groupColor,
		heldInAccountName = null,
		transactions = [],
		draggingCategoryId = null,
		draggingGroupName = null,
		draggingTxId = null,
		organizeMode = false,
		isFirstInGroup = false,
		isLastInGroup = false,
		availableGroups = [],
		selectedTransactionId = null,
		onCoverClick,
		onEditClick,
		onUpdatePlanned,
		onDropTransaction,
		onCategoryDragStart,
		onCategoryDragEnd,
		onReorderCategory,
		onMoveCategoryStep,
		onMoveCategoryToGroup,
		onSelectTransaction
	}: Props = $props();

	let rowContainerEl = $state<HTMLDivElement | null>(null);
	let upBtnEl = $state<HTMLButtonElement | null>(null);
	let downBtnEl = $state<HTMLButtonElement | null>(null);
	let isExpanded = $state(false);
	let isTxDragOver = $state(false);
	let categoryDropPosition = $state<'before' | 'after' | null>(null);
	let editingPlanned = $state(false);
	let plannedDraft = $state('');
	let savingPlanned = $state(false);
	let inputEl = $state<HTMLInputElement | null>(null);

	let prevDraggingCategoryId: string | null = null;
	$effect(() => {
		const current = draggingCategoryId;
		if (prevDraggingCategoryId !== null && current === null) {
			categoryDropPosition = null;
		}
		prevDraggingCategoryId = current;
	});

	let prevDraggingTxId: string | null = null;
	$effect(() => {
		const current = draggingTxId;
		if (prevDraggingTxId !== null && current === null) {
			isTxDragOver = false;
		}
		prevDraggingTxId = current;
	});

	let totalSpent = $derived(category.spentCleared + category.spentPending);
	let isFund = $derived(
		category.rolloverMode !== undefined && category.rolloverMode !== 'SWEEP'
	);
	let priorSaved = $derived(category.carryoverBalance ?? 0);
	let isOverspent = $derived(
		isFund ? category.remaining < 0 : category.remaining < 0 || totalSpent > category.effectiveTarget
	);

	let dueInfo = $derived.by(() => {
		if (isFund || !category.targetDate) return null;
		const day = Number(category.targetDate.slice(8, 10));
		if (!Number.isInteger(day) || day < 1 || day > 31) return null;

		const now = new Date();
		const year = now.getFullYear();
		const month = now.getMonth();
		const daysInMonth = new Date(year, month + 1, 0).getDate();
		const clampedDay = Math.min(day, daysInMonth);
		const dueDate = new Date(year, month, clampedDay);
		const today = new Date(year, month, now.getDate());
		const daysUntil = Math.round((dueDate.getTime() - today.getTime()) / 86_400_000);
		const monthShort = dueDate.toLocaleDateString('en-US', { month: 'short' });
		const isPaid =
			totalSpent > 0 &&
			(category.effectiveTarget <= 0 || totalSpent >= category.effectiveTarget - 0.005);

		return {
			label: isPaid ? 'Paid' : `Due ${monthShort} ${clampedDay}`,
			isPaid,
			isDueSoon: !isPaid && daysUntil <= 5
		};
	});

	let statusColor = $derived.by(() => {
		if (isOverspent) return 'red';
		if (isFund) return 'green';
		if (category.effectiveTarget <= 0) return totalSpent > 0 ? 'red' : 'green';
		const ratio = totalSpent / category.effectiveTarget;
		if (ratio > 0.8) return 'amber';
		return 'green';
	});

	let barScale = $derived(
		Math.max(category.effectiveTarget, category.originalPlanned, totalSpent, 1)
	);

	let clearedPct = $derived(Math.min(100, Math.max(0, (category.spentCleared / barScale) * 100)));
	let pendingPct = $derived(
		Math.min(100 - clearedPct, Math.max(0, (category.spentPending / barScale) * 100))
	);
	let showOriginalTick = $derived(
		Math.abs(category.originalPlanned - category.effectiveTarget) > 0.005 &&
			category.originalPlanned > 0
	);
	let tickPct = $derived(Math.min(100, (category.originalPlanned / barScale) * 100));
	let fundGoalPct = $derived(
		isFund && category.targetAmount && category.targetAmount > 0
			? Math.max(0, Math.min(100, (category.remaining / category.targetAmount) * 100))
			: 0
	);
	let fundGoalMet = $derived(
		isFund && category.targetAmount != null && category.targetAmount > 0 && category.remaining >= category.targetAmount
	);
	let fundPool = $derived(Math.max(priorSaved + category.effectiveTarget, 1));
	let fundTankPct = $derived(
		isFund ? Math.max(0, Math.min(100, (category.remaining / fundPool) * 100)) : 0
	);

	function fmt(n: number): string {
		const abs = Math.abs(n).toLocaleString('en-US', {
			minimumFractionDigits: 2,
			maximumFractionDigits: 2
		});
		return n < 0 ? `-$${abs}` : `$${abs}`;
	}

	function toggleExpanded() {
		if (organizeMode) return;
		isExpanded = !isExpanded;
	}

	function handleRowKeydown(e: KeyboardEvent) {
		if (organizeMode || e.target !== e.currentTarget) return;
		if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			toggleExpanded();
		}
	}

	function startEditPlanned(e: Event) {
		e.stopPropagation();
		if (organizeMode || !onUpdatePlanned || savingPlanned) return;
		plannedDraft = category.originalPlanned.toFixed(2);
		editingPlanned = true;
		queueMicrotask(() => {
			inputEl?.focus();
			inputEl?.select();
		});
	}

	async function commitPlanned() {
		if (!editingPlanned || !onUpdatePlanned) return;
		const parsed = Number(plannedDraft);
		editingPlanned = false;
		if (!Number.isFinite(parsed) || parsed < 0) return;
		if (Math.abs(parsed - category.originalPlanned) < 0.005) return;

		savingPlanned = true;
		try {
			await onUpdatePlanned(category.id, Math.round(parsed * 100) / 100);
		} finally {
			savingPlanned = false;
		}
	}

	function handlePlannedInputKeydown(e: KeyboardEvent) {
		e.stopPropagation();
		if (e.key === 'Enter') {
			e.preventDefault();
			void commitPlanned();
		} else if (e.key === 'Escape') {
			e.preventDefault();
			editingPlanned = false;
		}
	}

	function isGroupDragEvent(e: DragEvent): boolean {
		if (draggingGroupName !== null) return true;
		const types = e.dataTransfer?.types;
		if (!types) return false;
		return Array.from(types).includes('application/x-budget-group-name');
	}

	function isCategoryDragEvent(e: DragEvent): boolean {
		if (draggingCategoryId !== null) return true;
		const types = e.dataTransfer?.types;
		if (!types) return false;
		return Array.from(types).includes('application/x-budget-category-id');
	}

	async function handleMoveStep(direction: 'up' | 'down') {
		if (!onMoveCategoryStep) return;
		await onMoveCategoryStep(category.id, direction);
		queueMicrotask(() => {
			if (direction === 'up' && isFirstInGroup && downBtnEl && !downBtnEl.disabled) {
				downBtnEl.focus({ preventScroll: true });
			} else if (direction === 'down' && isLastInGroup && upBtnEl && !upBtnEl.disabled) {
				upBtnEl.focus({ preventScroll: true });
			}
		});
	}
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
	bind:this={rowContainerEl}
	data-testid="category-row-container"
	ondragover={(e) => {
		if (isGroupDragEvent(e)) return;
		if (isCategoryDragEvent(e) && onReorderCategory) {
			if (draggingCategoryId === category.id) return;
			e.preventDefault();
			if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
			const rect = e.currentTarget.getBoundingClientRect();
			const midY = rect.top + rect.height / 2;
			categoryDropPosition = e.clientY < midY ? 'before' : 'after';
			isTxDragOver = false;
			return;
		}
		if (!onDropTransaction) return;
		e.preventDefault();
		if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
		isTxDragOver = true;
		categoryDropPosition = null;
	}}
	ondragleave={(e) => {
		const related = e.relatedTarget as Node | null;
		if (related && e.currentTarget.contains(related)) return;
		isTxDragOver = false;
		categoryDropPosition = null;
	}}
	ondrop={async (e) => {
		const pos = categoryDropPosition ?? 'before';
		categoryDropPosition = null;
		isTxDragOver = false;
		if (isGroupDragEvent(e)) return;
		const draggedCatId =
			e.dataTransfer?.getData('application/x-budget-category-id') || draggingCategoryId;
		if (draggedCatId && onReorderCategory) {
			e.preventDefault();
			if (draggedCatId !== category.id) {
				await onReorderCategory(draggedCatId, category.id, pos);
			}
			return;
		}
		if (!onDropTransaction) return;
		e.preventDefault();
		const txId =
			e.dataTransfer?.getData('application/x-budget-tx-id') ||
			e.dataTransfer?.getData('text/plain');
		if (txId) {
			await onDropTransaction(txId, category.id);
		}
	}}
	style={embedded && groupColor && !isTxDragOver && categoryDropPosition === null
		? `background-color: ${groupColor.stroke}0a;`
		: undefined}
	class="relative {embedded
		? ''
		: 'rounded-xl border border-border bg-surface hover:border-ink-muted/50 overflow-hidden'} transition-[background-color,opacity,box-shadow] duration-150 {isTxDragOver
		? 'bg-accent-tint shadow-[inset_3px_0_0_var(--accent-pop)]'
		: categoryDropPosition !== null
			? 'bg-accent-tint/70'
			: ''} {draggingCategoryId === category.id ? 'opacity-50' : ''}"
>
	{#if categoryDropPosition !== null}
		<div
			aria-hidden="true"
			data-testid="category-drop-indicator"
			class="pointer-events-none absolute inset-x-3 h-[3px] rounded-full bg-accent-pop/75 z-10 {categoryDropPosition === 'before'
				? 'top-0 -translate-y-1/2'
				: 'bottom-0 translate-y-1/2'}"
		></div>
	{/if}
	<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div
		role={organizeMode ? undefined : 'button'}
		tabindex={organizeMode ? undefined : 0}
		aria-expanded={organizeMode ? undefined : isExpanded}
		onclick={toggleExpanded}
		onkeydown={handleRowKeydown}
		class="group {organizeMode ? '' : 'cursor-pointer'} transition-colors {embedded
			? 'px-3.5 sm:px-5 py-3 hover:bg-surface-hover/50'
			: 'p-3.5 hover:bg-surface-hover/40'}"
		data-testid="category-row"
	>
		<div class="grid {organizeMode
			? 'grid-cols-[minmax(0,1fr)_auto]'
			: 'grid-cols-[minmax(0,1fr)_76px_88px] sm:grid-cols-[minmax(0,1fr)_104px_116px]'} items-center gap-2 sm:gap-3">
			<!-- Name & Meta -->
			<div class="min-w-0 flex items-start sm:items-center gap-1.5 sm:gap-2">
				{#if onReorderCategory}
					<button
						type="button"
						data-testid="category-drag-handle"
						draggable={!editingPlanned}
						aria-label={`Reorder ${category.name}`}
						title="Drag to reorder (or use Up/Down arrow keys)"
						ondragstart={(e) => {
							if (editingPlanned) {
								e.preventDefault();
								return;
							}
							e.stopPropagation();
							e.dataTransfer?.setData('application/x-budget-category-id', category.id);
							if (e.dataTransfer) {
								e.dataTransfer.effectAllowed = 'move';
								if (rowContainerEl && typeof e.dataTransfer.setDragImage === 'function') {
									e.dataTransfer.setDragImage(rowContainerEl, 24, 24);
								}
							}
							onCategoryDragStart?.(category.id);
						}}
						ondragend={() => {
							categoryDropPosition = null;
							isTxDragOver = false;
							onCategoryDragEnd?.();
						}}
						onclick={(e) => e.stopPropagation()}
						onkeydown={(e) => {
							if (e.key === 'ArrowUp' && onMoveCategoryStep) {
								e.preventDefault();
								e.stopPropagation();
								void handleMoveStep('up');
							} else if (e.key === 'ArrowDown' && onMoveCategoryStep) {
								e.preventDefault();
								e.stopPropagation();
								void handleMoveStep('down');
							}
						}}
						class="hidden sm:inline-flex p-1 -ml-1 rounded text-ink-muted hover:text-ink-primary hover:bg-surface-hover cursor-grab active:cursor-grabbing shrink-0 touch-none select-none [-webkit-user-select:none]"
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
				{#if !organizeMode}
					<svg
						class="w-3.5 h-3.5 mt-0.5 sm:mt-0 shrink-0 text-ink-muted transition-transform duration-150 {isExpanded ? 'rotate-90' : ''}"
						viewBox="0 0 24 24"
						fill="none"
						stroke="currentColor"
						stroke-width="2"
						aria-hidden="true"
					>
						<path d="m9 18 6-6-6-6" />
					</svg>
				{/if}

				<div class="min-w-0 flex-1 flex flex-col sm:flex-row sm:flex-wrap sm:items-center gap-y-1 gap-x-2">
					<span class="text-left text-sm font-medium leading-snug text-ink-primary break-words sm:truncate">
						{category.name}
					</span>

					{#if !organizeMode && (dueInfo || isFund || category.spentPending > 0 || (category.pacing && !category.pacing.isMet) || onEditClick)}
						<div class="flex flex-wrap items-center gap-1.5">
							{#if isFund}
								<span
									data-testid="fund-badge"
									class="inline-flex items-center shrink-0 rounded-full px-2 py-0.5 text-[10px] font-semibold bg-accent-tint text-accent-pop border border-accent-pop/25"
								>
									Fund
								</span>
								{#if heldInAccountName}
									<span class="inline-flex items-center shrink-0 rounded-full px-2 py-0.5 text-[10px] font-medium bg-subtle text-ink-secondary border border-border">
										in {heldInAccountName}
									</span>
								{/if}
							{/if}
							{#if dueInfo}
								<span
									data-testid="due-status-badge"
									class="inline-flex items-center shrink-0 rounded-full px-2 py-0.5 text-[11px] font-semibold {dueInfo.isPaid
										? 'bg-status-good-bg text-status-good'
										: dueInfo.isDueSoon
											? 'bg-status-warn-bg text-status-warn border border-status-warn/30'
											: 'bg-subtle text-ink-secondary border border-border'}"
								>
									{dueInfo.label}
								</span>
							{/if}

							{#if category.spentPending > 0}
								<span
									data-testid="pending-badge"
									class="inline-flex items-center shrink-0 rounded-full bg-status-warn-bg text-status-warn px-1.5 py-0.5 text-[11px] font-mono font-medium"
								>
									+{fmt(category.spentPending)} pend
								</span>
							{/if}

							{#if category.pacing && !category.pacing.isMet}
								<span
									class="hidden sm:inline-flex items-center shrink-0 rounded-md bg-subtle px-1.5 py-0.5 text-[11px] font-medium text-ink-secondary border border-border"
								>
									Pace: {fmt(category.pacing.monthlyDue)}/mo
								</span>
							{/if}

							{#if onEditClick}
								<button
									type="button"
									data-testid="edit-category-btn"
									aria-label={`Edit ${category.name}`}
									onclick={(e) => {
										e.stopPropagation();
										onEditClick(category.id);
									}}
									class="px-2 py-1 rounded-md text-xs text-ink-muted hover:text-ink-primary hover:bg-surface-hover sm:opacity-0 sm:group-hover:opacity-100 sm:group-focus-within:opacity-100 sm:focus:opacity-100 transition-all shrink-0"
								>
									Edit
								</button>
							{/if}
						</div>
					{/if}
				</div>
			</div>

			{#if organizeMode}
				<div
					data-testid="category-organize-controls"
					class="flex items-center justify-end gap-1.5 shrink-0"
				>
					{#if availableGroups.length > 1 && onMoveCategoryToGroup}
						<select
							data-testid="move-category-group-select"
							aria-label={`Move ${category.name} to category`}
							value={category.groupName ?? ''}
							onchange={(e) => {
								const targetGroup = (e.currentTarget as HTMLSelectElement).value;
								if (targetGroup && targetGroup !== category.groupName) {
									void onMoveCategoryToGroup(category.id, targetGroup);
								}
							}}
							class="h-8 max-w-[120px] sm:max-w-[150px] rounded-lg border border-border bg-surface px-2 text-xs text-ink-primary focus:border-accent-pop focus:outline-none"
						>
							{#each availableGroups as grp (grp)}
								<option value={grp}>{grp}</option>
							{/each}
						</select>
					{/if}
					{#if onMoveCategoryStep}
						<button
							bind:this={upBtnEl}
							type="button"
							data-testid="move-category-up-btn"
							aria-label={`Move ${category.name} up`}
							disabled={isFirstInGroup}
							onclick={(e) => {
								e.stopPropagation();
								void handleMoveStep('up');
							}}
							class="h-8 w-8 inline-flex items-center justify-center rounded-lg border border-border bg-surface text-ink-primary hover:bg-surface-hover disabled:opacity-35 disabled:cursor-not-allowed transition-colors"
						>
							<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
								<path d="m18 15-6-6-6 6" />
							</svg>
						</button>
						<button
							bind:this={downBtnEl}
							type="button"
							data-testid="move-category-down-btn"
							aria-label={`Move ${category.name} down`}
							disabled={isLastInGroup}
							onclick={(e) => {
								e.stopPropagation();
								void handleMoveStep('down');
							}}
							class="h-8 w-8 inline-flex items-center justify-center rounded-lg border border-border bg-surface text-ink-primary hover:bg-surface-hover disabled:opacity-35 disabled:cursor-not-allowed transition-colors"
						>
							<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
								<path d="m6 9 6 6 6-6" />
							</svg>
						</button>
					{/if}
				</div>
			{:else}
			<!-- Planned Column (Muted, Inline Editable when onUpdatePlanned is provided) -->
			<div class="text-right">
				{#if editingPlanned}
					<input
						bind:this={inputEl}
						type="number"
						step="0.01"
						min="0"
						aria-label={`Planned budget for ${category.name}`}
						data-testid="inline-planned-input"
						bind:value={plannedDraft}
						onclick={(e) => e.stopPropagation()}
						onkeydown={handlePlannedInputKeydown}
						onblur={() => void commitPlanned()}
						class="w-full max-w-24 rounded-md border border-accent-pop bg-surface px-2 py-0.5 text-right text-xs font-mono font-medium text-ink-primary focus:outline-none"
					/>
				{:else if onUpdatePlanned}
					<button
						type="button"
						data-testid="inline-planned-btn"
						onclick={startEditPlanned}
						onkeydown={(e) => {
							if (e.key === 'Enter' || e.key === ' ') {
								e.preventDefault();
								startEditPlanned(e);
							}
						}}
						disabled={savingPlanned}
						title={showOriginalTick
							? `Base planned: ${fmt(category.originalPlanned)} (adjusted to ${fmt(category.effectiveTarget)})`
							: 'Click to edit planned budget'}
						class="inline-block rounded px-2 py-1 text-xs font-mono font-normal text-ink-muted hover:text-ink-primary hover:bg-surface-hover transition-colors"
					>
						{fmt(category.effectiveTarget)}
					</button>
				{:else}
					<span class="text-xs font-mono text-ink-muted">
						{fmt(category.effectiveTarget)}
					</span>
				{/if}
			</div>

			<!-- Remaining / Spent / Overspent Column (Bold Primary Figure) -->
			<div class="text-right shrink-0">
				{#if isOverspent}
					<button
						type="button"
						data-testid="overspent-badge"
						onclick={(e) => {
							e.stopPropagation();
							onCoverClick(category.id);
						}}
						class="inline-flex flex-wrap items-center justify-end gap-x-1 rounded-full bg-status-danger-bg px-2 py-0.5 text-[11px] sm:text-xs font-mono font-bold text-status-danger border border-status-danger/30 hover:opacity-90 transition-opacity"
					>
						<span>{fmt(category.remaining)}</span>
						<span class="font-sans text-[10px] uppercase tracking-wider underline">Cover</span>
					</button>
				{:else if isFund}
					{#if category.remaining > 0}
						<span
							data-testid="status-badge"
							class="inline-flex items-center gap-1 rounded-full bg-accent-tint px-2 py-0.5 text-xs font-mono font-bold text-accent-pop border border-accent-pop/25"
						>
							<span>{fmt(category.remaining)}</span>
							<span class="font-sans text-[10px] font-semibold opacity-80">left</span>
						</span>
					{:else}
						<span
							data-testid="status-badge"
							class="inline-flex items-center text-sm font-mono font-bold text-ink-muted"
						>
							{fmt(category.remaining)}
						</span>
					{/if}
				{:else if displayMode === 'spent'}
					<span
						data-testid="status-badge"
						class="inline-flex items-center text-sm font-mono font-bold {statusColor === 'amber'
							? 'text-status-warn'
							: 'text-ink-primary'}"
					>
						{fmt(totalSpent)}
					</span>
				{:else if statusColor === 'amber'}
					<span
						data-testid="status-badge"
						class="inline-flex items-center rounded-full bg-status-warn-bg px-2 py-0.5 text-xs font-mono font-bold text-status-warn"
					>
						{fmt(category.remaining)}
					</span>
				{:else}
					<span
						data-testid="status-badge"
						class="inline-flex items-center text-sm font-mono font-bold {totalSpent > 0 || category.effectiveTarget > 0
							? 'text-ink-primary'
							: 'text-ink-muted'}"
					>
						{fmt(category.remaining)}
					</span>
				{/if}
			</div>
			{/if}
		</div>

		{#if !organizeMode && isFund && (priorSaved > 0 || totalSpent !== 0)}
			<div
				data-testid="fund-meta-line"
				class="mt-1 ml-5 flex flex-wrap items-center gap-x-2 text-[11px] text-ink-secondary"
			>
				{#if priorSaved > 0}
					<span>Prior {fmt(priorSaved)}</span>
				{/if}
				{#if totalSpent > 0}
					<span>{priorSaved > 0 ? '· ' : ''}Spent {fmt(totalSpent)}</span>
				{:else if totalSpent < 0}
					<span class="text-status-good">{priorSaved > 0 ? '· ' : ''}+{fmt(Math.abs(totalSpent))} credit</span>
				{/if}
			</div>
		{/if}

		<!-- Goal Progress Bar (Funds with Target), Reverse Spend Tank Bar (Funds without Target), or Slim Spend Bar (Monthly) -->
		{#if !organizeMode && isFund && category.targetAmount && category.targetAmount > 0}
			<div class="mt-2 ml-5 space-y-1">
				<div class="flex items-center justify-between text-[10px] font-mono text-ink-secondary">
					<span>{fmt(category.remaining)} saved</span>
					<span>Goal: {fmt(category.targetAmount)}</span>
				</div>
				<div
					data-testid="fund-goal-progress-bar"
					class="relative h-1.5 overflow-hidden rounded-full bg-subtle"
				>
					<div
						data-testid="fund-goal-fill"
						class="absolute inset-y-0 left-0 transition-all duration-300 {isOverspent
							? 'bg-status-danger'
							: fundGoalMet
								? 'bg-status-good'
								: 'bg-accent-pop'}"
						style="width: {fundGoalPct}%; {!isOverspent && !fundGoalMet && groupColor
							? `background-color: ${groupColor.stroke};`
							: ''}"
					></div>
				</div>
			</div>
		{:else if !organizeMode && isFund}
			<div
				data-testid="fund-tank-progress-bar"
				class="mt-2 ml-5 relative h-1.5 overflow-hidden rounded-full bg-subtle"
			>
				<div
					data-testid="fund-tank-fill"
					class="absolute inset-y-0 left-0 transition-all duration-300 {isOverspent
						? 'bg-status-danger'
						: 'bg-accent-pop'}"
					style="width: {fundTankPct}%; {!isOverspent && groupColor
						? `background-color: ${groupColor.stroke};`
						: ''}"
				></div>
			</div>
		{:else if !organizeMode && !isFund}
			<div
				data-testid="variable-progress-bar"
				class="mt-2 ml-5 relative h-1.5 overflow-hidden rounded-full bg-subtle"
			>
				<!-- Layer 1: Solid cleared spend -->
				<div
					data-testid="bar-cleared"
					class="absolute inset-y-0 left-0 transition-all duration-300 {statusColor === 'red'
						? 'bg-status-danger'
						: statusColor === 'amber'
							? 'bg-status-warn'
							: 'bg-accent-pop'}"
					style="width: {clearedPct}%; {statusColor === 'green' && groupColor
						? `background-color: ${groupColor.stroke};`
						: ''}"
				></div>

				<!-- Layer 2: Striped pending spend -->
				{#if pendingPct > 0}
					<div
						data-testid="bar-pending"
						class="absolute inset-y-0 transition-all duration-300 bg-pending-stripe"
						style="left: {clearedPct}%; width: {pendingPct}%;"
					></div>
				{/if}

				<!-- Layer 3: Original planned tick mark -->
				{#if showOriginalTick}
					<div
						data-testid="bar-original-tick"
						title="Original baseline: {fmt(category.originalPlanned)}"
						class="absolute inset-y-0 w-0.5 bg-ink-primary/70 z-10"
						style="left: {tickPct}%"
					></div>
				{/if}
			</div>
		{/if}
	</div>

	{#if !organizeMode && isExpanded}
		<InlineActivityDrawer
			{transactions}
			{embedded}
			{selectedTransactionId}
			{onSelectTransaction}
		/>
	{/if}
</div>
