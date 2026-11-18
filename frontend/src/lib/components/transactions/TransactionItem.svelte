<script lang="ts">
	import type { TransactionItemModel } from '$lib/types/transaction';

	interface Props {
		transaction: TransactionItemModel;
		categoryName?: string;
		accountName?: string;
	}

	let { transaction, categoryName, accountName }: Props = $props();

	let isPending = $derived(transaction.status === 'PENDING');
	let isOfflineQueued = $derived(
		transaction.origin === 'MANUAL_OFFLINE' && transaction.syncStatus === 'QUEUED'
	);
	let hasSettledDelta = $derived(
		transaction.settledDelta != null && transaction.settledDelta !== 0
	);

	let displayMerchant = $derived(transaction.merchantClean || transaction.merchantRaw);
	let displayCategory = $derived(
		transaction.splits && transaction.splits.length > 0
			? `Split (${transaction.splits.length})`
			: categoryName || transaction.categoryName || 'Uncategorized'
	);

	function formatCurrency(val: number): string {
		const abs = Math.abs(val).toFixed(2);
		return val < 0 ? `-$${abs}` : `+$${abs}`;
	}
</script>

<div
	class="flex items-center justify-between gap-3 px-4 py-3 bg-slate-900 border border-slate-800 rounded-lg hover:border-slate-700 transition-colors"
	data-testid="transaction-item"
>
	<div class="flex items-start gap-3 min-w-0">
		<div class="flex flex-col min-w-0">
			<div class="flex items-center gap-2 flex-wrap">
				<span
					class="text-sm font-medium truncate {isPending
						? 'italic text-slate-400'
						: 'text-slate-100'}"
				>
					{displayMerchant}
				</span>

				{#if isPending}
					<span
						class="inline-flex items-center gap-1 text-[11px] italic text-slate-400 bg-slate-800/80 px-1.5 py-0.5 rounded"
						data-testid="badge-pending"
						title="Pending transaction"
					>
						<svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
							<circle cx="12" cy="12" r="10" />
							<polyline points="12 6 12 12 16 14" />
						</svg>
						Pending
					</span>
				{/if}

				{#if isOfflineQueued}
					<span
						class="inline-flex items-center gap-1 text-[11px] text-sky-300 bg-sky-500/15 border border-sky-500/30 px-1.5 py-0.5 rounded"
						data-testid="badge-offline-queued"
						title="Queued offline for sync"
					>
						<svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
							<path d="M12 16V8" />
							<path d="m9 11 3-3 3 3" />
							<path d="M20 16.58A5 5 0 0 0 18 7h-1.26A8 8 0 1 0 4 15.25" />
						</svg>
						Queued
					</span>
				{/if}

				{#if hasSettledDelta}
					<span
						class="inline-flex items-center gap-1 text-[11px] text-amber-300 bg-amber-500/15 border border-amber-500/30 px-1.5 py-0.5 rounded"
						data-testid="badge-settled-delta"
						title="Settled amount differs from authorization"
					>
						Δ {transaction.settledDelta! > 0 ? '+' : '-'}${Math.abs(transaction.settledDelta!).toFixed(2)}
					</span>
				{/if}
			</div>

			<div class="flex items-center gap-2 text-xs text-slate-400 mt-0.5">
				<span>{transaction.transactionDate}</span>
				<span>•</span>
				<span>{displayCategory}</span>
				{#if accountName || transaction.accountName}
					<span>•</span>
					<span>{accountName || transaction.accountName}</span>
				{/if}
			</div>
		</div>
	</div>

	<div class="text-right shrink-0">
		<span
			class="text-sm font-mono font-semibold {isPending
				? 'italic text-slate-400'
				: transaction.amount < 0
					? 'text-slate-100'
					: 'text-emerald-400'}"
		>
			{formatCurrency(transaction.amount)}
		</span>
	</div>
</div>
