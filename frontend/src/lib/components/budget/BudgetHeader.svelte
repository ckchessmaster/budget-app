<script lang="ts">
	import type { MonthSummary } from '$lib/types/budget';

	interface Props {
		currentMonth: string; // YYYY-MM
		summary: MonthSummary | null;
		onMonthChange: (newMonth: string) => void;
		onOpenIncomes: () => void;
		onOpenNewCategory: () => void;
	}

	let {
		currentMonth,
		summary,
		onMonthChange,
		onOpenIncomes,
		onOpenNewCategory
	}: Props = $props();

	let monthLabel = $derived.by(() => {
		const [y, m] = currentMonth.split('-').map(Number);
		if (!y || !m) return currentMonth;
		return new Date(Date.UTC(y, m - 1, 1)).toLocaleDateString('en-US', {
			month: 'long',
			year: 'numeric',
			timeZone: 'UTC'
		});
	});

	function shiftMonth(delta: number) {
		const [y, m] = currentMonth.split('-').map(Number);
		const d = new Date(Date.UTC(y, m - 1 + delta, 1));
		const next = `${d.getUTCFullYear()}-${String(d.getUTCMonth() + 1).padStart(2, '0')}`;
		onMonthChange(next);
	}

	function fmt(n: number): string {
		return Math.abs(n).toLocaleString('en-US', {
			minimumFractionDigits: 2,
			maximumFractionDigits: 2
		});
	}

	let readyState = $derived.by(() => {
		const val = summary?.readyToAssign ?? 0;
		if (Math.abs(val) < 0.005) return 'balanced';
		return val > 0 ? 'unassigned' : 'overassigned';
	});
</script>

<div class="space-y-4">
	<div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4 rounded-2xl border border-slate-800 bg-slate-900 p-5">
		<div class="flex items-center gap-3">
			<button
				type="button"
				aria-label="Previous month"
				data-testid="prev-month-btn"
				onclick={() => shiftMonth(-1)}
				class="rounded-lg border border-slate-700 bg-slate-950 px-2.5 py-1.5 text-sm text-slate-300 hover:bg-slate-800 hover:text-white transition-colors"
			>
				&lsaquo;
			</button>
			<h2 class="text-lg font-bold text-white tracking-tight min-w-[160px] text-center">
				{monthLabel}
			</h2>
			<button
				type="button"
				aria-label="Next month"
				data-testid="next-month-btn"
				onclick={() => shiftMonth(1)}
				class="rounded-lg border border-slate-700 bg-slate-950 px-2.5 py-1.5 text-sm text-slate-300 hover:bg-slate-800 hover:text-white transition-colors"
			>
				&rsaquo;
			</button>
		</div>

		<div class="flex flex-wrap items-center gap-2">
			<button
				type="button"
				onclick={onOpenIncomes}
				class="rounded-lg border border-slate-700 bg-slate-800/80 px-3 py-1.5 text-xs font-medium text-slate-200 hover:bg-slate-700 transition-colors"
			>
				Planned Income: ${fmt(summary?.totalIncomeExpected ?? 0)}
			</button>
			<button
				type="button"
				onclick={onOpenNewCategory}
				class="rounded-lg bg-emerald-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-emerald-500 transition-colors"
			>
				+ Add Category
			</button>
		</div>
	</div>

	<!-- Ready to Assign Banner -->
	<div
		data-testid="ready-to-assign-banner"
		class="flex flex-col sm:flex-row sm:items-center justify-between gap-2 rounded-xl border px-4 py-3 text-sm font-medium {readyState === 'balanced'
			? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300'
			: readyState === 'unassigned'
				? 'border-amber-500/30 bg-amber-500/10 text-amber-300'
				: 'border-rose-500/40 bg-rose-500/15 text-rose-300'}"
	>
		<div>
			{#if readyState === 'balanced'}
				<span>Zero-Based Balanced — Ready to Assign: $0.00</span>
			{:else if readyState === 'unassigned'}
				<span>You have ${fmt(summary?.readyToAssign ?? 0)} to assign</span>
			{:else}
				<span>Over-assigned by ${fmt(summary?.readyToAssign ?? 0)}</span>
			{/if}
		</div>

		{#if summary}
			<div class="flex items-center gap-4 text-xs opacity-90">
				<span>Allocated: ${fmt(summary.totalAllocated)}</span>
				<span>Spent: ${fmt(summary.totalSpentCleared + summary.totalSpentPending)}</span>
				{#if summary.isUnbalanced}
					<span class="rounded bg-rose-500/30 px-2 py-0.5 text-[11px] font-semibold text-rose-200">
						Overspent Categories
					</span>
				{/if}
			</div>
		{/if}
	</div>
</div>
