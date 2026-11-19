<script lang="ts">
	export interface CategoryRowItem {
		id: string;
		name: string;
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
		onCoverClick: (categoryId: string) => void;
		onEditClick?: (categoryId: string) => void;
		onUpdatePlanned?: (categoryId: string, newPlanned: number) => Promise<void> | void;
		onFilterTransactions?: (categoryId: string) => void;
	}

	let {
		category,
		displayMode = 'remaining',
		embedded = false,
		onCoverClick,
		onEditClick,
		onUpdatePlanned,
		onFilterTransactions
	}: Props = $props();

	let editingPlanned = $state(false);
	let plannedDraft = $state('');
	let savingPlanned = $state(false);
	let inputEl = $state<HTMLInputElement | null>(null);

	let totalSpent = $derived(category.spentCleared + category.spentPending);
	let isOverspent = $derived(category.remaining < 0 || totalSpent > category.effectiveTarget);

	let statusColor = $derived.by(() => {
		if (isOverspent) return 'red';
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

	function handleRowClick() {
		onEditClick?.(category.id);
	}

	function handleRowKeydown(e: KeyboardEvent) {
		if (!onEditClick) return;
		if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			onEditClick(category.id);
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

<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div
	role={onEditClick ? 'button' : undefined}
	tabindex={onEditClick ? 0 : undefined}
	onclick={onEditClick ? handleRowClick : undefined}
	onkeydown={onEditClick ? handleRowKeydown : undefined}
	class="group transition-colors {embedded
		? 'px-5 py-3 hover:bg-slate-50/80 dark:hover:bg-slate-800/40'
		: 'rounded-xl border border-slate-200 dark:border-slate-800/80 bg-white dark:bg-slate-900/70 p-3.5 hover:border-slate-300 dark:hover:border-slate-700'} {onEditClick
		? 'cursor-pointer'
		: ''}"
	data-testid="category-row"
>
	<div class="grid grid-cols-[minmax(0,1fr)_96px_96px] sm:grid-cols-[minmax(0,1fr)_116px_116px] items-center gap-3">
		<!-- Name & Meta -->
		<div class="min-w-0">
			<div class="flex items-center gap-2">
				<span
					class="truncate text-left text-sm font-medium text-slate-900 dark:text-slate-100 group-hover:text-emerald-600 dark:group-hover:text-emerald-400 transition-colors"
				>
					{category.name}
				</span>

				{#if category.pacing && !category.pacing.isMet}
					<span
						class="hidden sm:inline-flex items-center rounded-md bg-slate-100 dark:bg-slate-800 px-1.5 py-0.5 text-[11px] font-medium text-slate-600 dark:text-slate-300 border border-slate-200 dark:border-slate-700"
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
						class="text-[11px] text-slate-400 hover:text-sky-600 dark:hover:text-sky-400 opacity-0 group-hover:opacity-100 focus:opacity-100 transition-opacity"
					>
						Txns
					</button>
				{/if}

				{#if onEditClick}
					<span
						class="text-[11px] text-slate-400 dark:text-slate-500 opacity-0 group-hover:opacity-100 transition-opacity"
					>
						Edit
					</span>
				{/if}
			</div>
		</div>

		<!-- Planned Column (Inline Editable when onUpdatePlanned is provided) -->
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
					class="w-24 rounded-md border border-sky-500 bg-white dark:bg-slate-950 px-2 py-0.5 text-right text-sm font-mono font-semibold text-slate-900 dark:text-white focus:outline-none"
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
					class="inline-block rounded px-1.5 py-0.5 text-sm font-mono font-semibold text-sky-600 dark:text-sky-400 hover:bg-sky-50 dark:hover:bg-sky-500/10 transition-colors"
				>
					{fmt(category.effectiveTarget)}
				</button>
			{:else}
				<span class="text-xs font-mono text-slate-500 dark:text-slate-400">
					{fmt(category.effectiveTarget)}
				</span>
			{/if}
		</div>

		<!-- Remaining / Spent / Overspent Column -->
		<div class="text-right shrink-0">
			{#if isOverspent}
				<button
					type="button"
					data-testid="overspent-badge"
					onclick={(e) => {
						e.stopPropagation();
						onCoverClick(category.id);
					}}
					class="inline-flex items-center gap-1 rounded-full bg-rose-50 dark:bg-rose-500/15 px-2.5 py-0.5 text-xs font-mono font-semibold text-rose-600 dark:text-rose-400 border border-rose-200 dark:border-rose-500/40 hover:bg-rose-100 dark:hover:bg-rose-500/25 transition-colors"
				>
					<span>{fmt(category.remaining)}</span>
					<span class="font-sans text-[10px] uppercase tracking-wider underline">Cover</span>
				</button>
			{:else if displayMode === 'spent'}
				<span
					data-testid="status-badge"
					class="inline-flex items-center text-sm font-mono font-semibold {statusColor === 'amber'
						? 'text-amber-600 dark:text-amber-300'
						: 'text-slate-700 dark:text-slate-200'}"
				>
					{fmt(totalSpent)}
				</span>
			{:else if statusColor === 'amber'}
				<span
					data-testid="status-badge"
					class="inline-flex items-center rounded-full bg-amber-50 dark:bg-amber-500/15 px-2 py-0.5 text-xs font-mono font-semibold text-amber-700 dark:text-amber-300 border border-amber-200 dark:border-amber-500/30"
				>
					{fmt(category.remaining)}
				</span>
			{:else}
				<span
					data-testid="status-badge"
					class="inline-flex items-center text-sm font-mono font-semibold {totalSpent > 0 || category.effectiveTarget > 0
						? 'text-slate-800 dark:text-slate-100'
						: 'text-slate-400 dark:text-slate-500'}"
				>
					{fmt(category.remaining)}
				</span>
			{/if}
		</div>
	</div>

	<!-- Compact Meta Subline + 3-Layer Progress Bar -->
	<div class="mt-2 flex items-center gap-3">
		<div class="relative h-2 flex-1 overflow-hidden rounded-full bg-slate-100 dark:bg-slate-800">
			<!-- Layer 1: Solid cleared spend -->
			<div
				data-testid="bar-cleared"
				class="absolute inset-y-0 left-0 transition-all duration-300 {statusColor === 'red'
					? 'bg-rose-500'
					: statusColor === 'amber'
						? 'bg-amber-400'
						: 'bg-emerald-500'}"
				style="width: {clearedPct}%"
			></div>

			<!-- Layer 2: Striped pending spend -->
			{#if pendingPct > 0}
				<div
					data-testid="bar-pending"
					class="absolute inset-y-0 transition-all duration-300 opacity-80"
					style="left: {clearedPct}%; width: {pendingPct}%; background-image: repeating-linear-gradient(45deg, {statusColor === 'red'
						? '#f43f5e'
						: statusColor === 'amber'
							? '#fbbf24'
							: '#10b981'} 0px, {statusColor === 'red'
						? '#f43f5e'
						: statusColor === 'amber'
							? '#fbbf24'
							: '#10b981'} 4px, transparent 4px, transparent 8px);"
				></div>
			{/if}

			<!-- Layer 3: Original planned tick mark -->
			{#if showOriginalTick}
				<div
					data-testid="bar-original-tick"
					title="Original baseline: {fmt(category.originalPlanned)}"
					class="absolute inset-y-0 w-0.5 bg-slate-900/70 dark:bg-white/90 shadow-[0_0_4px_rgba(255,255,255,0.8)] z-10"
					style="left: {tickPct}%"
				></div>
			{/if}
		</div>

		{#if category.spentPending > 0}
			<span class="shrink-0 text-[10px] text-slate-400 dark:text-slate-500">
				{fmt(category.spentPending)} pending
			</span>
		{/if}
	</div>
</div>
