<script lang="ts">
	import type { InlineActivityItem, PlannedIncome } from '$lib/types/budget';
	import IncomeModal from './IncomeModal.svelte';
	import InlineActivityDrawer from './InlineActivityDrawer.svelte';

	interface Props {
		yearMonth: string; // YYYY-MM-01
		incomes: PlannedIncome[];
		receivedAmount: number;
		inflowTransactions?: InlineActivityItem[];
		selectedTransactionId?: string | null;
		draggingGroupName?: string | null;
		draggingTxId?: string | null;
		onSelectTransaction?: (txId: string) => void;
		onDropTransaction?: (txId: string, incomeId: string) => Promise<void> | void;
		onChanged: () => void;
	}

	let {
		yearMonth,
		incomes,
		receivedAmount,
		inflowTransactions = [],
		selectedTransactionId = null,
		draggingGroupName = null,
		draggingTxId = null,
		onSelectTransaction,
		onDropTransaction,
		onChanged
	}: Props = $props();

	let expandedRows = $state<Record<string, boolean>>({});
	let dragOverIncomeId = $state<string | null>(null);

	let prevDraggingTxId: string | null = null;
	$effect(() => {
		const current = draggingTxId;
		if (prevDraggingTxId !== null && current === null) {
			dragOverIncomeId = null;
		}
		prevDraggingTxId = current;
	});

	let isIncomeModalOpen = $state(false);
	let editingIncome = $state<PlannedIncome | null>(null);

	let totalPlanned = $derived(incomes.reduce((sum, i) => sum + i.amount, 0));

	let incomeRows = $derived.by(() => {
		const explicitlyAssigned = inflowTransactions.filter((tx) => Boolean(tx.plannedIncomeId));
		const unassigned = inflowTransactions.filter((tx) => !tx.plannedIncomeId);

		const preMatched = incomes.map((inc) => {
			const explicitMatched = explicitlyAssigned.filter((tx) => tx.plannedIncomeId === inc.id);
			const needle = inc.name.trim().toLowerCase();
			const fuzzyMatched = needle
				? unassigned.filter((tx) => {
						const m = tx.merchant.toLowerCase();
						return m.includes(needle) || (m.length >= 3 && needle.includes(m));
					})
				: [];
			return {
				income: inc,
				matched: [...explicitMatched, ...fuzzyMatched]
			};
		});

		const consumedIds = new Set<string>();
		let matchedSum = 0;
		for (const entry of preMatched) {
			for (const tx of entry.matched) {
				if (!consumedIds.has(tx.id)) {
					consumedIds.add(tx.id);
					matchedSum += tx.amount;
				}
			}
		}

		let remainingPool = Math.max(0, receivedAmount - Math.max(0, matchedSum));
		return preMatched.map(({ income: inc, matched }) => {
			let rowReceived = 0;
			let drawerTransactions: InlineActivityItem[] = [];

			if (matched.length > 0) {
				rowReceived = matched.reduce((sum, tx) => sum + tx.amount, 0);
				drawerTransactions = matched;
			} else if (explicitlyAssigned.length === 0 && incomes.length === 1) {
				rowReceived = remainingPool > inc.amount ? remainingPool : Math.min(inc.amount, remainingPool);
				remainingPool = Math.max(0, remainingPool - rowReceived);
				drawerTransactions = unassigned;
			} else if (inc.amount > 0) {
				rowReceived = Math.min(inc.amount, remainingPool);
				remainingPool = Math.max(0, remainingPool - rowReceived);
			}

			const isPaid = inc.amount > 0 ? rowReceived >= inc.amount - 0.005 : rowReceived > 0;
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

	function openAddModal() {
		editingIncome = null;
		isIncomeModalOpen = true;
	}

	function openEditModal(inc: PlannedIncome) {
		editingIncome = inc;
		isIncomeModalOpen = true;
	}
</script>

<section
	aria-label="Income"
	data-testid="income-card"
	class="rounded-2xl border border-border bg-surface shadow-xs overflow-hidden"
>
	<!-- Header -->
	<div class="px-3.5 sm:px-5 pt-4 pb-3 border-b border-border grid grid-cols-[minmax(0,1fr)_76px_88px] sm:grid-cols-[minmax(0,1fr)_104px_116px] items-end gap-2 sm:gap-3">
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
			<div class="px-3.5 sm:px-5 py-3 text-xs text-ink-muted">
				No planned income sources for this month yet.
			</div>
		{:else}
			{#each incomeRows as row (row.income.id)}
				<!-- svelte-ignore a11y_no_static_element_interactions -->
				<div
					data-testid="income-row-container"
					ondragover={(e) => {
						if (!onDropTransaction || draggingGroupName !== null) return;
						const types = e.dataTransfer?.types ? Array.from(e.dataTransfer.types) : [];
						if (
							types.includes('application/x-budget-category-id') ||
							types.includes('application/x-budget-group-name')
						) {
							return;
						}
						e.preventDefault();
						if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
						dragOverIncomeId = row.income.id;
					}}
					ondragleave={(e) => {
						const related = e.relatedTarget as Node | null;
						if (related && e.currentTarget.contains(related)) return;
						if (dragOverIncomeId === row.income.id) {
							dragOverIncomeId = null;
						}
					}}
					ondrop={async (e) => {
						if (dragOverIncomeId === row.income.id) {
							dragOverIncomeId = null;
						}
						if (!onDropTransaction || draggingGroupName !== null) return;
						const catId = e.dataTransfer?.getData('application/x-budget-category-id');
						const groupName = e.dataTransfer?.getData('application/x-budget-group-name');
						if (catId || groupName) return;
						e.preventDefault();
						const txId =
							e.dataTransfer?.getData('application/x-budget-tx-id') ||
							e.dataTransfer?.getData('text/plain');
						if (txId) {
							await onDropTransaction(txId, row.income.id);
						}
					}}
					class="transition-colors duration-150 {dragOverIncomeId === row.income.id
						? 'bg-accent-tint shadow-[inset_3px_0_0_var(--accent-pop)]'
						: ''}"
				>
					<div
						role="button"
						tabindex="0"
						aria-expanded={Boolean(expandedRows[row.income.id])}
						data-testid="income-row"
						onclick={() => toggleRow(row.income.id)}
						onkeydown={(e) => handleRowKeydown(e, row.income.id)}
						class="group cursor-pointer px-3.5 sm:px-5 py-2.5 grid grid-cols-[minmax(0,1fr)_76px_88px] sm:grid-cols-[minmax(0,1fr)_104px_116px] items-center gap-2 sm:gap-3 hover:bg-surface-hover/50 transition-colors"
					>
						<div class="min-w-0 flex items-start sm:items-center gap-1.5 sm:gap-2">
							<svg
								class="w-3.5 h-3.5 mt-0.5 sm:mt-0 shrink-0 text-ink-muted transition-transform duration-150 {expandedRows[row.income.id] ? 'rotate-90' : ''}"
								viewBox="0 0 24 24"
								fill="none"
								stroke="currentColor"
								stroke-width="2"
								aria-hidden="true"
							>
								<path d="m9 18 6-6-6-6" />
							</svg>
							<div class="min-w-0 flex-1 flex flex-col sm:flex-row sm:flex-wrap sm:items-center gap-y-1 gap-x-2">
								<span class="text-sm font-medium leading-snug text-ink-primary break-words sm:truncate">
									{row.income.name}
								</span>
								<div class="flex flex-wrap items-center gap-1.5">
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
										data-testid="edit-income-btn"
										onclick={(e) => {
											e.stopPropagation();
											openEditModal(row.income);
										}}
										aria-label={`Edit ${row.income.name}`}
										class="px-2 py-1 rounded-md text-xs text-ink-muted hover:text-ink-primary hover:bg-surface-hover sm:opacity-0 sm:group-hover:opacity-100 sm:group-focus-within:opacity-100 sm:focus:opacity-100 transition-all shrink-0"
									>
										Edit
									</button>
								</div>
							</div>
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
						<InlineActivityDrawer
							transactions={row.drawerTransactions}
							embedded={true}
							{selectedTransactionId}
							{onSelectTransaction}
						/>
					{/if}
				</div>
			{/each}
		{/if}
	</div>

	<!-- Footer -->
	<div class="px-3.5 sm:px-5 py-2 bg-subtle border-t border-border">
		<button
			type="button"
			onclick={openAddModal}
			class="px-2 py-1.5 -ml-2 rounded-lg text-xs font-semibold text-accent-pop hover:bg-surface-hover transition-colors"
		>
			+ Add Income
		</button>
	</div>
</section>

<IncomeModal
	isOpen={isIncomeModalOpen}
	{yearMonth}
	{editingIncome}
	onClose={() => (isIncomeModalOpen = false)}
	onSuccess={() => {
		isIncomeModalOpen = false;
		onChanged();
	}}
/>
