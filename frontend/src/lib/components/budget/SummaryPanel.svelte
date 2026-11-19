<script lang="ts">
	import { getGroupColor } from '$lib/utils/budgetColors';
	import type { CategoryBudget, MonthSummary } from '$lib/types/budget';

	type SummaryMetric = 'planned' | 'spent' | 'remaining';

	interface Props {
		categories: CategoryBudget[];
		summary: MonthSummary | null;
		selectedGroup: string | null;
		onSelectGroup: (groupName: string | null) => void;
	}

	let { categories, summary, selectedGroup, onSelectGroup }: Props = $props();

	let activeMetric = $state<SummaryMetric>('planned');
	let hoveredGroup = $state<string | null>(null);

	const METRICS: Array<{ id: SummaryMetric; label: string; totalLabel: string }> = [
		{ id: 'planned', label: 'PLANNED', totalLabel: 'TOTAL PLANNED' },
		{ id: 'spent', label: 'SPENT', totalLabel: 'TOTAL SPENT' },
		{ id: 'remaining', label: 'REMAINING', totalLabel: 'TOTAL REMAINING' }
	];

	const RADIUS = 58;
	const CIRCUMFERENCE = 2 * Math.PI * RADIUS;

	let groupAggregates = $derived.by(() => {
		const map = new Map<
			string,
			{ groupName: string; planned: number; spent: number; remaining: number }
		>();
		for (const cat of categories) {
			const cur = map.get(cat.groupName) ?? {
				groupName: cat.groupName,
				planned: 0,
				spent: 0,
				remaining: 0
			};
			cur.planned += cat.effectiveTarget;
			cur.spent += cat.spentCleared + cat.spentPending;
			cur.remaining += cat.remaining;
			map.set(cat.groupName, cur);
		}
		return Array.from(map.values()).map((g, idx) => {
			const displayValue =
				activeMetric === 'planned'
					? g.planned
					: activeMetric === 'spent'
						? g.spent
						: g.remaining;
			const chartValue = Math.max(0, displayValue);
			return {
				...g,
				color: getGroupColor(g.groupName, idx),
				displayValue,
				chartValue
			};
		});
	});

	let totalDisplayValue = $derived(
		groupAggregates.reduce((sum, g) => sum + g.displayValue, 0)
	);
	let totalChartValue = $derived(
		groupAggregates.reduce((sum, g) => sum + g.chartValue, 0)
	);

	let slices = $derived.by(() => {
		if (totalChartValue <= 0) return [];
		let offset = 0;
		return groupAggregates
			.filter((g) => g.chartValue > 0)
			.map((g) => {
				const pct = (g.chartValue / totalChartValue) * 100;
				const length = (g.chartValue / totalChartValue) * CIRCUMFERENCE;
				const slice = {
					groupName: g.groupName,
					color: g.color,
					value: g.displayValue,
					pct,
					length,
					offset
				};
				offset += length;
				return slice;
			});
	});

	let focusedGroupName = $derived(hoveredGroup ?? selectedGroup);
	let focusedGroup = $derived(
		focusedGroupName
			? groupAggregates.find((g) => g.groupName === focusedGroupName) ?? null
			: null
	);

	function fmtMoney(n: number): string {
		const abs = Math.abs(n).toLocaleString('en-US', {
			minimumFractionDigits: 2,
			maximumFractionDigits: 2
		});
		return n < 0 ? `-$${abs}` : `$${abs}`;
	}

	function pctOfTotal(chartValue: number): number {
		if (totalChartValue <= 0) return 0;
		return Math.round((chartValue / totalChartValue) * 100);
	}

	function handleTabKeydown(e: KeyboardEvent, idx: number) {
		if (e.key === 'ArrowRight') {
			e.preventDefault();
			activeMetric = METRICS[(idx + 1) % METRICS.length].id;
		} else if (e.key === 'ArrowLeft') {
			e.preventDefault();
			activeMetric = METRICS[(idx - 1 + METRICS.length) % METRICS.length].id;
		}
	}
</script>

<div class="flex flex-col h-full" data-testid="summary-panel">
	<!-- Donut Chart -->
	<div class="px-5 pt-5 pb-4 flex flex-col items-center">
		<div class="relative w-48 h-48 flex items-center justify-center">
			<svg viewBox="0 0 160 160" class="w-full h-full -rotate-90" aria-label="Budget breakdown chart">
				<!-- Track -->
				<circle
					cx="80"
					cy="80"
					r={RADIUS}
					fill="none"
					stroke-width="18"
					class="stroke-slate-100 dark:stroke-slate-800"
				/>

				{#each slices as slice (slice.groupName)}
					<circle
						cx="80"
						cy="80"
						r={RADIUS}
						fill="none"
						stroke={slice.color.stroke}
						stroke-width={focusedGroupName === slice.groupName ? 22 : 18}
						stroke-dasharray={`${slice.length} ${CIRCUMFERENCE}`}
						stroke-dashoffset={-slice.offset}
						class="cursor-pointer transition-all duration-200 focus:outline-none {focusedGroupName &&
						focusedGroupName !== slice.groupName
							? 'opacity-45'
							: 'opacity-100'}"
						role="button"
						tabindex="0"
						aria-label={`${slice.groupName}: ${fmtMoney(slice.value)}`}
						onmouseenter={() => (hoveredGroup = slice.groupName)}
						onmouseleave={() => (hoveredGroup = null)}
						onfocus={() => (hoveredGroup = slice.groupName)}
						onblur={() => (hoveredGroup = null)}
						onclick={() =>
							onSelectGroup(selectedGroup === slice.groupName ? null : slice.groupName)}
						onkeydown={(e) => {
							if (e.key === 'Enter' || e.key === ' ') {
								e.preventDefault();
								onSelectGroup(selectedGroup === slice.groupName ? null : slice.groupName);
							}
						}}
					/>
				{/each}
			</svg>

			<!-- Center Readout -->
			<div class="absolute inset-0 flex flex-col items-center justify-center text-center px-6 pointer-events-none">
				{#if focusedGroup}
					<span class="text-[10px] font-bold uppercase tracking-wider text-slate-500 dark:text-slate-400 truncate max-w-full">
						{focusedGroup.groupName}
					</span>
					<span
						data-testid="donut-center-value"
						class="text-lg font-mono font-bold text-slate-900 dark:text-white mt-0.5"
					>
						{fmtMoney(focusedGroup.displayValue)}
					</span>
					{#if totalChartValue > 0}
						<span class="text-[11px] font-medium text-slate-500 dark:text-slate-400">
							{pctOfTotal(focusedGroup.chartValue)}%
						</span>
					{/if}
				{:else}
					<span class="text-[10px] font-bold uppercase tracking-wider text-slate-500 dark:text-slate-400">
						{METRICS.find((m) => m.id === activeMetric)?.totalLabel}
					</span>
					<span
						data-testid="donut-center-value"
						class="text-xl font-mono font-bold text-slate-900 dark:text-white mt-0.5"
					>
						{fmtMoney(totalDisplayValue)}
					</span>
				{/if}
			</div>
		</div>
	</div>

	<!-- Metric Sub-Tabs -->
	<div
		role="tablist"
		aria-label="Summary metric"
		class="grid grid-cols-3 border-b border-slate-200 dark:border-slate-800 px-3"
	>
		{#each METRICS as metric, idx}
			{@const isSelected = activeMetric === metric.id}
			<button
				type="button"
				role="tab"
				aria-selected={isSelected}
				tabindex={isSelected ? 0 : -1}
				onclick={() => (activeMetric = metric.id)}
				onkeydown={(e) => handleTabKeydown(e, idx)}
				class="relative py-2.5 text-[11px] font-bold tracking-wider transition-colors {isSelected
					? 'text-slate-900 dark:text-white after:content-[""] after:absolute after:inset-x-2 after:bottom-0 after:h-0.5 after:bg-slate-900 dark:after:bg-emerald-400'
					: 'text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200'}"
			>
				{metric.label}
			</button>
		{/each}
	</div>

	<!-- Accessible Breakdown List -->
	<div class="flex-1 overflow-y-auto overscroll-contain-scroll divide-y divide-slate-100 dark:divide-slate-800/80 px-4 py-1">
		{#if groupAggregates.length === 0}
			<p class="py-6 text-center text-xs text-slate-500 dark:text-slate-400">
				No budget groups yet.
			</p>
		{:else}
			{#each groupAggregates as group (group.groupName)}
				{@const isSelected = selectedGroup === group.groupName}
				<button
					type="button"
					data-testid="summary-group-row"
					onclick={() => onSelectGroup(isSelected ? null : group.groupName)}
					onmouseenter={() => (hoveredGroup = group.groupName)}
					onmouseleave={() => (hoveredGroup = null)}
					class="w-full py-2.5 px-2 -mx-2 rounded-lg flex items-center justify-between gap-3 text-left transition-colors {isSelected
						? 'bg-slate-100 dark:bg-slate-800/80'
						: 'hover:bg-slate-50 dark:hover:bg-slate-800/40'}"
				>
					<span class="flex items-center gap-2 min-w-0">
						<span class="w-2.5 h-2.5 rounded-full shrink-0 {group.color.dotClass}" aria-hidden="true"></span>
						<span class="truncate text-xs font-semibold {group.color.textClass}">
							{group.groupName}
						</span>
					</span>

					<span class="shrink-0 flex items-center gap-2 font-mono text-xs">
						<span
							class="font-semibold {group.displayValue < 0
								? 'text-rose-600 dark:text-rose-400'
								: 'text-slate-800 dark:text-slate-100'}"
						>
							{fmtMoney(group.displayValue)}
						</span>
						<span class="w-10 text-right text-slate-400 dark:text-slate-500">
							({pctOfTotal(group.chartValue)}%)
						</span>
					</span>
				</button>
			{/each}
		{/if}
	</div>

	<!-- Month Snapshot Footer -->
	{#if summary}
		<div class="grid grid-cols-3 gap-2 px-4 py-3 border-t border-slate-200 dark:border-slate-800 bg-slate-50/70 dark:bg-slate-900/80 text-center">
			<div>
				<p class="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-400">Income</p>
				<p class="text-xs font-mono font-semibold text-emerald-600 dark:text-emerald-400">
					{fmtMoney(summary.totalIncomeExpected)}
				</p>
			</div>
			<div>
				<p class="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-400">Allocated</p>
				<p class="text-xs font-mono font-semibold text-slate-800 dark:text-slate-100">
					{fmtMoney(summary.totalAllocated)}
				</p>
			</div>
			<div>
				<p class="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-400">Spent</p>
				<p class="text-xs font-mono font-semibold text-slate-800 dark:text-slate-100">
					{fmtMoney(summary.totalSpentCleared + summary.totalSpentPending)}
				</p>
			</div>
		</div>
	{/if}
</div>
