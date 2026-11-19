<script lang="ts">
	import { createPlannedIncome, deletePlannedIncome } from '$lib/api/budget';
	import type { PlannedIncome } from '$lib/types/budget';

	interface Props {
		yearMonth: string; // YYYY-MM-01
		incomes: PlannedIncome[];
		receivedAmount: number;
		onChanged: () => void;
	}

	let { yearMonth, incomes, receivedAmount, onChanged }: Props = $props();

	let showAddForm = $state(false);
	let name = $state('');
	let amount = $state('');
	let isSubmitting = $state(false);
	let errorMsg = $state('');

	let totalPlanned = $derived(incomes.reduce((sum, i) => sum + i.amount, 0));
	let remainingToReceive = $derived(totalPlanned - receivedAmount);
	let progressPct = $derived(
		totalPlanned > 0 ? Math.min(100, Math.max(0, (receivedAmount / totalPlanned) * 100)) : 0
	);

	function fmt(n: number): string {
		const abs = Math.abs(n).toLocaleString('en-US', {
			minimumFractionDigits: 2,
			maximumFractionDigits: 2
		});
		return n < 0 ? `-$${abs}` : `$${abs}`;
	}

	async function handleAdd(e: SubmitEvent) {
		e.preventDefault();
		const parsed = Number(amount);
		if (!name.trim() || !Number.isFinite(parsed) || parsed <= 0) {
			errorMsg = 'Enter a source name and an amount greater than $0.00.';
			return;
		}
		isSubmitting = true;
		errorMsg = '';
		try {
			await createPlannedIncome({
				year_month: yearMonth,
				name: name.trim(),
				amount: parsed
			});
			name = '';
			amount = '';
			showAddForm = false;
			onChanged();
		} catch (err: any) {
			errorMsg = err?.message || 'Failed to add planned income.';
		} finally {
			isSubmitting = false;
		}
	}

	async function handleDelete(id: string) {
		isSubmitting = true;
		errorMsg = '';
		try {
			await deletePlannedIncome(id);
			onChanged();
		} catch (err: any) {
			errorMsg = err?.message || 'Failed to remove planned income.';
		} finally {
			isSubmitting = false;
		}
	}
</script>

<section
	aria-label="Income"
	data-testid="income-card"
	class="rounded-2xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 shadow-xs overflow-hidden"
>
	<!-- Header -->
	<div class="px-5 pt-4 pb-3 border-b border-slate-100 dark:border-slate-800/80">
		<div class="grid grid-cols-[minmax(0,1fr)_96px_96px] sm:grid-cols-[minmax(0,1fr)_116px_116px] items-end gap-3">
			<div>
				<h3 class="inline-block text-base font-bold text-slate-900 dark:text-white border-b-2 border-emerald-500 pb-0.5">
					Income
				</h3>
			</div>
			<div class="text-right">
				<div class="text-sm font-mono font-bold text-slate-900 dark:text-white">
					{fmt(totalPlanned)}
				</div>
				<div class="text-[11px] font-medium text-slate-500 dark:text-slate-400">Planned</div>
			</div>
			<div class="text-right">
				<div class="text-sm font-mono font-bold text-slate-900 dark:text-white">
					{fmt(receivedAmount)}
				</div>
				<div class="text-[11px] font-medium text-slate-500 dark:text-slate-400">
					Received
					{#if totalPlanned > 0 && remainingToReceive > 0}
						<span class="hidden sm:inline text-slate-400 dark:text-slate-500">
							({fmt(remainingToReceive)} left)
						</span>
					{/if}
				</div>
			</div>
		</div>

		<!-- Income progress bar -->
		<div class="mt-2.5 h-1.5 w-full rounded-full bg-slate-100 dark:bg-slate-800 overflow-hidden">
			<div
				data-testid="income-progress-bar"
				class="h-full rounded-full bg-emerald-500 transition-all duration-300"
				style="width: {progressPct}%"
			></div>
		</div>
	</div>

	<!-- Income Rows -->
	<div class="divide-y divide-slate-100 dark:divide-slate-800/80">
		{#if incomes.length === 0}
			<div class="px-5 py-3 text-xs text-slate-500 dark:text-slate-400">
				No planned income sources for this month yet.
			</div>
		{:else}
			{#each incomes as inc (inc.id)}
				<div class="group px-5 py-2.5 grid grid-cols-[minmax(0,1fr)_96px_96px] sm:grid-cols-[minmax(0,1fr)_116px_116px] items-center gap-3 hover:bg-slate-50/70 dark:hover:bg-slate-800/40 transition-colors">
					<div class="min-w-0 flex items-center gap-2">
						<span class="truncate text-sm font-medium text-slate-800 dark:text-slate-100">
							{inc.name}
						</span>
						<button
							type="button"
							disabled={isSubmitting}
							onclick={() => handleDelete(inc.id)}
							aria-label={`Remove ${inc.name}`}
							class="text-[11px] text-rose-600 dark:text-rose-400 opacity-0 group-hover:opacity-100 focus:opacity-100 hover:underline transition-opacity"
						>
							Remove
						</button>
					</div>
					<div class="text-right text-sm font-mono font-semibold text-sky-600 dark:text-sky-400">
						{fmt(inc.amount)}
					</div>
					<div class="text-right text-xs font-mono text-slate-500 dark:text-slate-400">
						Planned
					</div>
				</div>
			{/each}
		{/if}
	</div>

	<!-- Footer / Inline Add Income -->
	<div class="px-5 py-2.5 bg-slate-50/60 dark:bg-slate-900/60 border-t border-slate-100 dark:border-slate-800/80">
		{#if showAddForm}
			<form onsubmit={handleAdd} class="space-y-2">
				<div class="flex flex-col sm:flex-row gap-2">
					<input
						type="text"
						placeholder="Paycheck / Income source"
						aria-label="Income source name"
						bind:value={name}
						required
						class="flex-1 rounded-lg border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-3 py-1.5 text-sm text-slate-900 dark:text-white focus:border-emerald-500 focus:outline-none"
					/>
					<input
						type="number"
						step="0.01"
						min="0.01"
						placeholder="2500.00"
						aria-label="Income amount"
						bind:value={amount}
						required
						class="w-full sm:w-32 rounded-lg border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-3 py-1.5 text-sm font-mono text-slate-900 dark:text-white focus:border-emerald-500 focus:outline-none"
					/>
					<div class="flex items-center gap-1.5">
						<button
							type="submit"
							disabled={isSubmitting}
							class="rounded-lg bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 px-3 py-1.5 text-xs font-semibold text-white transition-colors"
						>
							{isSubmitting ? 'Adding...' : 'Add'}
						</button>
						<button
							type="button"
							onclick={() => {
								showAddForm = false;
								errorMsg = '';
							}}
							class="rounded-lg border border-slate-300 dark:border-slate-700 px-2.5 py-1.5 text-xs text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
						>
							Cancel
						</button>
					</div>
				</div>
				{#if errorMsg}
					<p class="text-xs text-rose-600 dark:text-rose-400">{errorMsg}</p>
				{/if}
			</form>
		{:else}
			<button
				type="button"
				onclick={() => (showAddForm = true)}
				class="text-xs font-semibold text-sky-600 hover:text-sky-700 dark:text-sky-400 dark:hover:text-sky-300 transition-colors"
			>
				+ Add Income
			</button>
		{/if}
	</div>
</section>
