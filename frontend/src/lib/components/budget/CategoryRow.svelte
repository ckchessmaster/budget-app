<script lang="ts">
	import type { InlineActivityItem } from '$lib/types/budget';
	import { isFixedCategory } from '$lib/utils/budgetCategoryKind';
	import InlineActivityDrawer from './InlineActivityDrawer.svelte';

	export interface CategoryRowItem {
		id: string;
		name: string;
		groupName?: string;
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
		transactions?: InlineActivityItem[];
		onCoverClick: (categoryId: string) => void;
		onEditClick?: (categoryId: string) => void;
		onUpdatePlanned?: (categoryId: string, newPlanned: number) => Promise<void> | void;
		onFilterTransactions?: (categoryId: string) => void;
	}

	let {
		category,
		displayMode = 'remaining',
		embedded = false,
		transactions = [],
		onCoverClick,
		onEditClick,
		onUpdatePlanned,
		onFilterTransactions
	}: Props = $props();

	let isExpanded = $state(false);
	let editingPlanned = $state(false);
	let plannedDraft = $state('');
	let savingPlanned = $state(false);
	let inputEl = $state<HTMLInputElement | null>(null);

	let totalSpent = $derived(category.spentCleared + category.spentPending);
	let isOverspent = $derived(category.remaining < 0 || totalSpent > category.effectiveTarget);

	let isFixed = $derived(
		category.kind ? category.kind === 'fixed' : isFixedCategory(category.name, category.groupName)
	);
	let isFixedPaid = $derived(
		totalSpent > 0 && (category.effectiveTarget <= 0 || totalSpent >= category.effectiveTarget - 0.005)
	);

	let statusColor = $derived.by(() => {
		if (isOverspent) return 'red';
		if (isFixed) return 'green';
		if (category.effectiveTarget <= 0) return totalSpent > 0 ? 'red' : 'green';
		const ratio = totalSpent / category.effectiveTarget;
		if (ratio > 0.8) return 'amber';
		return 'green';
	});

	let barScale = $derived(
		Math.max(category.effectiveTarget, category.originalPlanned, totalSpent, 1)
	);

	let clearedPct = $derived(Math.min(100, (category.spentCleared / barScale) * 100));
	let pendingPct = $derived(
		Math.min(100 - clearedPct, (category.spentPending / barScale) * 100)
	);
	let showOriginalTick = $derived(
		Math.abs(category.originalPlanned - category.effectiveTarget) > 0.005 &&
			category.originalPlanned > 0
	);
	let tickPct = $derived(Math.min(100, (category.originalPlanned / barScale) * 100));

	function fmt(n: number): string {
		const abs = Math.abs(n).toLocaleString('en-US', {
			minimumFractionDigits: 2,
			maximumFractionDigits: 2
		});
		return n < 0 ? `-$${abs}` : `$${abs}`;
	}

	function toggleExpanded() {
		isExpanded = !isExpanded;
	}

	function handleRowKeydown(e: KeyboardEvent) {
		if (e.target !== e.currentTarget) return;
		if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			toggleExpanded();
		}
	}

	function startEditPlanned(e: Event) {
		e.stopPropagation();
		if (!onUpdatePlanned || savingPlanned) return;
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
</script>

<div
	data-testid="category-row-container"
	class={embedded
		? ''
		: 'rounded-xl border border-border bg-surface hover:border-ink-muted/50 transition-colors overflow-hidden'}
>
	<div
		role="button"
		tabindex="0"
		aria-expanded={isExpanded}
		onclick={toggleExpanded}
		onkeydown={handleRowKeydown}
		class="group cursor-pointer transition-colors {embedded
			? 'px-5 py-3 hover:bg-surface-hover/50'
			: 'p-3.5 hover:bg-surface-hover/40'}"
		data-testid="category-row"
	>
		<div class="grid grid-cols-[minmax(0,1fr)_88px_96px] sm:grid-cols-[minmax(0,1fr)_104px_116px] items-center gap-3">
			<!-- Name & Meta -->
			<div class="min-w-0 flex items-center gap-2">
				<svg
					class="w-3.5 h-3.5 shrink-0 text-ink-muted transition-transform duration-150 {isExpanded ? 'rotate-90' : ''}"
					viewBox="0 0 24 24"
					fill="none"
					stroke="currentColor"
					stroke-width="2"
					aria-hidden="true"
				>
					<path d="m9 18 6-6-6-6" />
				</svg>

				<span class="truncate text-left text-sm font-medium text-ink-primary">
					{category.name}
				</span>

				{#if isFixed}
					<span
						data-testid="fixed-status-badge"
						class="inline-flex items-center shrink-0 rounded-full px-2 py-0.5 text-[11px] font-semibold {isFixedPaid
							? 'bg-status-good-bg text-status-good'
							: 'bg-subtle text-ink-secondary border border-border'}"
					>
						{isFixedPaid ? 'Paid' : 'Upcoming'}
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

				{#if onFilterTransactions}
					<button
						type="button"
						onclick={(e) => {
							e.stopPropagation();
							onFilterTransactions(category.id);
						}}
						title={`Filter transactions for ${category.name}`}
						aria-label={`Filter transactions for ${category.name}`}
						class="text-[11px] text-ink-muted hover:text-accent-pop opacity-0 group-hover:opacity-100 focus:opacity-100 transition-opacity shrink-0"
					>
						Txns
					</button>
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
						class="text-[11px] text-ink-muted hover:text-ink-primary opacity-0 group-hover:opacity-100 focus:opacity-100 transition-opacity shrink-0"
					>
						Edit
					</button>
				{/if}
			</div>

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
						class="w-24 rounded-md border border-accent-pop bg-surface px-2 py-0.5 text-right text-xs font-mono font-medium text-ink-primary focus:outline-none"
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
						class="inline-block rounded px-1.5 py-0.5 text-xs font-mono font-normal text-ink-muted hover:text-ink-primary hover:bg-surface-hover transition-colors"
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
						class="inline-flex items-center gap-1 rounded-full bg-status-danger-bg px-2.5 py-0.5 text-xs font-mono font-bold text-status-danger border border-status-danger/30 hover:opacity-90 transition-opacity"
					>
						<span>{fmt(category.remaining)}</span>
						<span class="font-sans text-[10px] uppercase tracking-wider underline">Cover</span>
					</button>
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
		</div>

		<!-- Slim Progress Bar (Variable Categories Only) -->
		{#if !isFixed}
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
					style="width: {clearedPct}%"
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

	{#if isExpanded}
		<InlineActivityDrawer {transactions} {embedded} />
	{/if}
</div>
