<script lang="ts">
	import { createPlannedIncome, deletePlannedIncome } from '$lib/api/budget';
	import type { InlineActivityItem, PlannedIncome } from '$lib/types/budget';
	import InlineActivityDrawer from './InlineActivityDrawer.svelte';

	interface Props {
		yearMonth: string; // YYYY-MM-01
		incomes: PlannedIncome[];
		receivedAmount: number;
		inflowTransactions?: InlineActivityItem[];
		onChanged: () => void;
	}

	let {
		yearMonth,
		incomes,
		receivedAmount,
		inflowTransactions = [],
		onChanged
	}: Props = $props();

	let expandedRows = $state<Record<string, boolean>>({});
	let showAddForm = $state(false);
	let name = $state('');
	let amount = $state('');
	let isSubmitting = $state(false);
	let errorMsg = $state('');

	let totalPlanned = $derived(incomes.reduce((sum, i) => sum + i.amount, 0));

	let incomeRows = $derived.by(() => {
		let remainingPool = Math.max(0, receivedAmount);
		return incomes.map((inc) => {
			const needle = inc.name.trim().toLowerCase();
			const matched = needle
				? inflowTransactions.filter((tx) => {
						const m = tx.merchant.toLowerCase();
						return m.includes(needle) || (m.length >= 3 && needle.includes(m));
					})
				: [];

			let rowReceived = 0;
			if (matched.length > 0) {
				rowReceived = matched.reduce((sum, tx) => sum + Math.max(0, tx.amount), 0);
			} else if (incomes.length === 1 && remainingPool > inc.amount) {
				rowReceived = remainingPool;
				remainingPool = 0;
			} else {
				rowReceived = Math.min(inc.amount, remainingPool);
				remainingPool = Math.max(0, remainingPool - rowReceived);
			}

			const isPaid = inc.amount > 0 ? rowReceived >= inc.amount - 0.005 : rowReceived > 0;
			const drawerTransactions = matched.length > 0 ? matched : inflowTransactions;
			return {
				income: inc,
				rowReceived,
				isPaid,
				drawerTransactions
			};
		});
	});

	function fmt(n: number): string {
		const abs = Math.abs(n).toLocaleString('en-US', {
			minimumFractionDigits: 2,
			maximumFractionDigits: 2
		});
		return n < 0 ? `-$${abs}` : `$${abs}`;
	}

	function toggleRow(id: string) {
		expandedRows[id] = !expandedRows[id];
	}

	function handleRowKeydown(e: KeyboardEvent, id: string) {
		if (e.target !== e.currentTarget) return;
		if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			toggleRow(id);
		}
	}

	async function handleAdd(e: SubmitEvent) {
		e.preventDefault();
		const parsed = Number(amount);
		if (!name.trim() || !Number.isFinite(parsed) || parsed < 0) {
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
	class="rounded-2xl border border-border bg-surface shadow-xs overflow-hidden"
>
	<!-- Header -->
	<div class="px-5 pt-4 pb-3 border-b border-border grid grid-cols-[minmax(0,1fr)_88px_96px] sm:grid-cols-[minmax(0,1fr)_104px_116px] items-end gap-3">
		<div>
			<h3 class="inline-block text-base font-bold text-ink-primary border-b-2 border-accent-pop pb-0.5">
				Income
			</h3>
		</div>
		<div class="text-right">
			<div class="text-xs font-mono font-medium text-ink-muted">
				{fmt(totalPlanned)}
			</div>
			<div class="text-[11px] font-medium text-ink-muted">Planned</div>
		</div>
		<div class="text-right">
			<div
				data-testid="income-received-total"
				class="text-sm sm:text-base font-mono font-bold {totalPlanned > 0 && receivedAmount >= totalPlanned
					? 'text-status-good'
					: 'text-ink-primary'}"
			>
				{fmt(receivedAmount)}
			</div>
			<div class="text-[11px] font-medium text-ink-muted">Received</div>
		</div>
	</div>

	<!-- Income Rows -->
	<div class="divide-y divide-border">
		{#if incomes.length === 0}
			<div class="px-5 py-3 text-xs text-ink-muted">
				No planned income sources for this month yet.
			</div>
		{:else}
			{#each incomeRows as row (row.income.id)}
				<div>
					<div
						role="button"
						tabindex="0"
						aria-expanded={Boolean(expandedRows[row.income.id])}
						data-testid="income-row"
						onclick={() => toggleRow(row.income.id)}
						onkeydown={(e) => handleRowKeydown(e, row.income.id)}
						class="group cursor-pointer px-5 py-2.5 grid grid-cols-[minmax(0,1fr)_88px_96px] sm:grid-cols-[minmax(0,1fr)_104px_116px] items-center gap-3 hover:bg-surface-hover/50 transition-colors"
					>
						<div class="min-w-0 flex items-center gap-2">
							<svg
								class="w-3.5 h-3.5 shrink-0 text-ink-muted transition-transform duration-150 {expandedRows[row.income.id] ? 'rotate-90' : ''}"
								viewBox="0 0 24 24"
								fill="none"
								stroke="currentColor"
								stroke-width="2"
								aria-hidden="true"
							>
								<path d="m9 18 6-6-6-6" />
							</svg>
							<span class="truncate text-sm font-medium text-ink-primary">
								{row.income.name}
							</span>
							<span
								data-testid="income-status-badge"
								class="inline-flex items-center shrink-0 rounded-full px-2 py-0.5 text-[11px] font-semibold {row.isPaid
									? 'bg-status-good-bg text-status-good'
									: 'bg-subtle text-ink-secondary border border-border'}"
							>
								{row.isPaid ? 'Paid' : 'Upcoming'}
							</span>
							<button
								type="button"
								disabled={isSubmitting}
								onclick={(e) => {
									e.stopPropagation();
									void handleDelete(row.income.id);
								}}
								aria-label={`Remove ${row.income.name}`}
								class="text-[11px] text-status-danger opacity-0 group-hover:opacity-100 focus:opacity-100 hover:underline transition-opacity shrink-0"
							>
								Remove
							</button>
						</div>
						<div class="text-right text-xs font-mono text-ink-muted">
							{fmt(row.income.amount)}
						</div>
						<div
							class="text-right text-sm font-mono font-bold {row.isPaid
								? 'text-status-good'
								: row.rowReceived > 0
									? 'text-ink-primary'
									: 'text-ink-muted'}"
						>
							{fmt(row.rowReceived)}
						</div>
					</div>

					{#if expandedRows[row.income.id]}
						<InlineActivityDrawer transactions={row.drawerTransactions} embedded={true} />
					{/if}
				</div>
			{/each}
		{/if}
	</div>

	<!-- Footer / Inline Add Income -->
	<div class="px-5 py-2.5 bg-subtle border-t border-border">
		{#if showAddForm}
			<form onsubmit={handleAdd} class="space-y-2">
				<div class="flex flex-col sm:flex-row gap-2">
					<input
						type="text"
						placeholder="Paycheck / Income source"
						aria-label="Income source name"
						bind:value={name}
						required
						class="flex-1 rounded-lg border border-border bg-surface px-3 py-1.5 text-sm text-ink-primary focus:border-accent-pop focus:outline-none"
					/>
					<input
						type="number"
						step="0.01"
						min="0"
						placeholder="2500.00"
						aria-label="Income amount"
						bind:value={amount}
						required
						class="w-full sm:w-32 rounded-lg border border-border bg-surface px-3 py-1.5 text-sm font-mono text-ink-primary focus:border-accent-pop focus:outline-none"
					/>
					<div class="flex items-center gap-1.5">
						<button
							type="submit"
							disabled={isSubmitting}
							class="rounded-lg bg-accent-primary text-accent-primary-text hover:opacity-90 disabled:opacity-50 px-3 py-1.5 text-xs font-semibold transition-opacity"
						>
							{isSubmitting ? 'Adding...' : 'Add'}
						</button>
						<button
							type="button"
							onclick={() => {
								showAddForm = false;
								errorMsg = '';
							}}
							class="rounded-lg border border-border px-2.5 py-1.5 text-xs text-ink-secondary hover:bg-surface-hover transition-colors"
						>
							Cancel
						</button>
					</div>
				</div>
				{#if errorMsg}
					<p class="text-xs text-status-danger">{errorMsg}</p>
				{/if}
			</form>
		{:else}
			<button
				type="button"
				onclick={() => (showAddForm = true)}
				class="text-xs font-semibold text-accent-pop hover:underline transition-colors"
			>
				+ Add Income
			</button>
		{/if}
	</div>
</section>
