<script lang="ts">
	import type { MonthSummary } from '$lib/types/budget';
	import type { SolvencyReport } from '$lib/types/transaction';

	interface Props {
		currentMonth: string; // YYYY-MM
		summary: MonthSummary | null;
		solvency?: SolvencyReport | null;
		accountCount?: number;
		isOffline?: boolean;
		queuedCount?: number;
		syncing?: boolean;
		onSyncNow?: () => void;
		onMonthChange: (newMonth: string) => void;
		onOpenIncomes?: () => void;
		onOpenNewCategory: () => void;
	}

	let {
		currentMonth,
		summary,
		solvency = null,
		accountCount,
		isOffline = false,
		queuedCount = 0,
		syncing = false,
		onSyncNow,
		onMonthChange,
		onOpenIncomes,
		onOpenNewCategory
	}: Props = $props();

	let thisMonth = $derived.by(() => {
		const now = new Date();
		return `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}`;
	});

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

	function fmtSigned(n: number): string {
		const abs = fmt(n);
		return n < 0 ? `-$${abs}` : `$${abs}`;
	}

	let readyState = $derived.by(() => {
		const val = summary?.readyToAssign ?? 0;
		if (Math.abs(val) < 0.005) return 'balanced';
		return val > 0 ? 'unassigned' : 'overassigned';
	});
</script>

<div class="space-y-3">
	<!-- Top Month & Action Bar -->
	<div class="flex flex-wrap items-center justify-between gap-3">
		<div class="flex items-center gap-2">
			<div class="inline-flex items-center rounded-xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 p-1 shadow-xs">
				<button
					type="button"
					aria-label="Previous month"
					data-testid="prev-month-btn"
					onclick={() => shiftMonth(-1)}
					class="rounded-lg px-2.5 py-1 text-sm text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 hover:text-slate-900 dark:hover:text-white transition-colors"
				>
					&lsaquo;
				</button>
				<h2 class="text-sm sm:text-base font-bold text-slate-900 dark:text-white tracking-tight min-w-[148px] text-center px-2">
					{monthLabel}
				</h2>
				<button
					type="button"
					aria-label="Next month"
					data-testid="next-month-btn"
					onclick={() => shiftMonth(1)}
					class="rounded-lg px-2.5 py-1 text-sm text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 hover:text-slate-900 dark:hover:text-white transition-colors"
				>
					&rsaquo;
				</button>
			</div>

			{#if currentMonth !== thisMonth}
				<button
					type="button"
					onclick={() => onMonthChange(thisMonth)}
					class="rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 px-2.5 py-1.5 text-xs font-medium text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-800 transition-colors"
				>
					Today
				</button>
			{/if}
		</div>

		<div class="flex flex-wrap items-center gap-2">
			{#if isOffline}
				<span class="text-xs px-2.5 py-1 rounded-full bg-amber-50 text-amber-800 border border-amber-200 dark:bg-amber-500/15 dark:text-amber-300 dark:border-amber-500/30">
					Offline Mode
				</span>
			{/if}

			{#if queuedCount > 0 && onSyncNow}
				<button
					type="button"
					onclick={onSyncNow}
					disabled={syncing || isOffline}
					class="text-xs px-3 py-1.5 rounded-lg bg-sky-600 hover:bg-sky-500 disabled:opacity-50 text-white font-medium transition-colors"
				>
					{syncing ? 'Syncing...' : `Sync Queued (${queuedCount})`}
				</button>
			{/if}

			{#if onOpenIncomes}
				<button
					type="button"
					onclick={onOpenIncomes}
					class="hidden sm:inline-flex rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 px-3 py-1.5 text-xs font-medium text-slate-700 dark:text-slate-200 hover:bg-slate-50 dark:hover:bg-slate-800 transition-colors"
				>
					Incomes: ${fmt(summary?.totalIncomeExpected ?? 0)}
				</button>
			{/if}

			<button
				type="button"
				onclick={onOpenNewCategory}
				class="rounded-lg bg-emerald-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-emerald-500 shadow-xs transition-colors"
			>
				+ Category
			</button>
		</div>
	</div>

	<!-- Hero Zero-Based Status Pill (EveryDollar-inspired) -->
	<div
		data-testid="ready-to-assign-banner"
		class="flex flex-col sm:flex-row sm:items-center justify-between gap-2 rounded-2xl border bg-white dark:bg-slate-900 px-5 py-3.5 shadow-xs {readyState === 'overassigned'
			? 'border-rose-300 dark:border-rose-500/40 bg-rose-50/70 dark:bg-rose-500/10'
			: 'border-slate-200 dark:border-slate-800'}"
	>
		<div class="flex items-baseline gap-2 flex-wrap">
			{#if readyState === 'balanced'}
				<span class="text-lg font-bold font-mono text-emerald-600 dark:text-emerald-400">$0.00</span>
				<span class="text-sm font-medium text-slate-700 dark:text-slate-200">left to budget — Zero-Based Balanced</span>
			{:else if readyState === 'unassigned'}
				<span class="text-lg font-bold font-mono text-emerald-600 dark:text-emerald-400">
					${fmt(summary?.readyToAssign ?? 0)}
				</span>
				<span class="text-sm font-medium text-slate-700 dark:text-slate-200">left to budget</span>
			{:else}
				<span class="text-lg font-bold font-mono text-rose-600 dark:text-rose-400">
					-${fmt(summary?.readyToAssign ?? 0)}
				</span>
				<span class="text-sm font-medium text-rose-700 dark:text-rose-300">over-budgeted</span>
			{/if}
		</div>

		{#if summary}
			<div class="flex flex-wrap items-center gap-3 text-xs text-slate-500 dark:text-slate-400">
				<span>
					Planned: <strong class="font-mono text-slate-800 dark:text-slate-200">${fmt(summary.totalAllocated)}</strong>
				</span>
				<span>
					Spent: <strong class="font-mono text-slate-800 dark:text-slate-200">${fmt(summary.totalSpentCleared + summary.totalSpentPending)}</strong>
				</span>
				{#if summary.isUnbalanced}
					<span class="rounded-full bg-rose-100 text-rose-700 border border-rose-200 dark:bg-rose-500/20 dark:text-rose-300 dark:border-rose-500/30 px-2 py-0.5 text-[11px] font-semibold">
						Overspent Categories
					</span>
				{/if}
			</div>
		{/if}
	</div>

	<!-- Compact Solvency Strip -->
	{#if solvency}
		<div
			data-testid="solvency-strip"
			class="flex flex-wrap items-center justify-between gap-3 rounded-xl border px-4 py-2.5 text-xs {solvency.isFloatingDebt
				? 'border-rose-200 bg-rose-50/70 dark:border-rose-500/30 dark:bg-rose-500/10'
				: 'border-slate-200 bg-white/80 dark:border-slate-800 dark:bg-slate-900/70'}"
		>
			<div class="flex flex-wrap items-center gap-x-5 gap-y-1">
				<div class="flex items-center gap-1.5">
					<span class="text-slate-500 dark:text-slate-400">Liquid Cash:</span>
					<span class="font-mono font-semibold text-emerald-600 dark:text-emerald-400">
						{fmtSigned(solvency.totalLiquidCash)}
					</span>
				</div>
				<div class="flex items-center gap-1.5">
					<span class="text-slate-500 dark:text-slate-400">CC Debt:</span>
					<span class="font-mono font-semibold text-slate-800 dark:text-slate-200">
						{fmtSigned(solvency.totalCreditCardDebt)}
					</span>
				</div>
				<div class="flex items-center gap-1.5">
					<span class="text-slate-500 dark:text-slate-400">Solvency Buffer:</span>
					<span
						class="font-mono font-semibold {solvency.isFloatingDebt
							? 'text-rose-600 dark:text-rose-400'
							: 'text-emerald-600 dark:text-emerald-400'}"
					>
						{fmtSigned(solvency.solvencyBuffer)}
					</span>
					{#if solvency.isFloatingDebt}
						<span class="rounded bg-rose-100 text-rose-700 dark:bg-rose-500/20 dark:text-rose-300 px-1.5 py-0.5 text-[10px] font-semibold">
							Floating Debt
						</span>
					{/if}
				</div>
			</div>

			<a
				href="/accounts"
				class="font-semibold text-sky-600 hover:text-sky-700 dark:text-sky-400 dark:hover:text-sky-300 transition-colors"
			>
				Manage Accounts{typeof accountCount === 'number' ? ` (${accountCount})` : ''} &rarr;
			</a>
		</div>
	{/if}
</div>
