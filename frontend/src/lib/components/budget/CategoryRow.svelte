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
		onCoverClick: (categoryId: string) => void;
		onEditClick?: (categoryId: string) => void;
	}

	let { category, onCoverClick, onEditClick }: Props = $props();

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
</script>

<div
	class="group rounded-xl border border-slate-800/80 bg-slate-900/70 p-3.5 transition-colors hover:border-slate-700"
	data-testid="category-row"
>
	<div class="flex items-center justify-between gap-3">
		<div class="min-w-0 flex-1">
			<div class="flex items-center gap-2">
				<button
					type="button"
					onclick={() => onEditClick?.(category.id)}
					class="truncate text-left text-sm font-medium text-slate-100 hover:text-emerald-400 transition-colors"
				>
					{category.name}
				</button>
				{#if category.pacing && !category.pacing.isMet}
					<span
						class="hidden sm:inline-flex items-center rounded-md bg-slate-800 px-1.5 py-0.5 text-[11px] font-medium text-slate-300 border border-slate-700"
					>
						Pace: {fmt(category.pacing.monthlyDue)}/mo
					</span>
				{/if}
			</div>
			<div class="mt-0.5 flex items-center gap-2 text-xs text-slate-400">
				<span>Spent: {fmt(totalSpent)}</span>
				{#if category.spentPending > 0}
					<span class="text-slate-500">({fmt(category.spentPending)} pending)</span>
				{/if}
				<span class="text-slate-600">•</span>
				<span>Target: {fmt(category.effectiveTarget)}</span>
			</div>
		</div>

		<div class="shrink-0">
			{#if isOverspent}
				<button
					type="button"
					data-testid="overspent-badge"
					onclick={() => onCoverClick(category.id)}
					class="inline-flex items-center gap-1.5 rounded-full bg-rose-500/15 px-3 py-1 text-xs font-semibold text-rose-400 border border-rose-500/40 hover:bg-rose-500/25 transition-colors"
				>
					<span>{fmt(category.remaining)}</span>
					<span class="text-[10px] uppercase tracking-wider underline">Cover</span>
				</button>
			{:else if statusColor === 'amber'}
				<span
					data-testid="status-badge"
					class="inline-flex items-center rounded-full bg-amber-500/15 px-2.5 py-1 text-xs font-semibold text-amber-300 border border-amber-500/30"
				>
					{fmt(category.remaining)} left
				</span>
			{:else}
				<span
					data-testid="status-badge"
					class="inline-flex items-center rounded-full bg-emerald-500/15 px-2.5 py-1 text-xs font-semibold text-emerald-400 border border-emerald-500/30"
				>
					{fmt(category.remaining)} left
				</span>
			{/if}
		</div>
	</div>

	<!-- 3-Layer Progress Bar: 1) Solid Cleared, 2) Striped Pending, 3) Original Baseline Tick -->
	<div class="relative mt-2.5 h-2.5 w-full overflow-hidden rounded-full bg-slate-800">
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
				class="absolute inset-y-0 w-0.5 bg-white/90 shadow-[0_0_4px_rgba(255,255,255,0.8)] z-10"
				style="left: {tickPct}%"
			></div>
		{/if}
	</div>
</div>
