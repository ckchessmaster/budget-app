<script lang="ts">
	import type { TransactionItemModel } from '$lib/types/transaction';

	interface Props {
		transaction: TransactionItemModel;
		categoryName?: string;
		accountName?: string;
		isOnBudget?: boolean;
	}

	let { transaction, categoryName, accountName, isOnBudget = true }: Props = $props();

	let isPending = $derived(transaction.status === 'PENDING');
	let isNew = $derived(
		!isPending &&
			transaction.categoryId === null &&
			!transaction.isTransfer &&
			!(transaction.splits && transaction.splits.length > 0) &&
			isOnBudget
	);
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
	class="flex items-center justify-between gap-3 px-4 py-3 bg-surface border border-border rounded-lg hover:bg-surface-hover/50 transition-colors"
	data-testid="transaction-item"
>
	<div class="flex items-start gap-3 min-w-0">
		<div class="flex flex-col min-w-0">
			<div class="flex items-center gap-2 flex-wrap">
				<span
					class="text-sm font-medium truncate {isPending
						? 'italic text-ink-secondary'
						: 'text-ink-primary'}"
				>
					{displayMerchant}
				</span>

				{#if isPending}
					<span
						class="inline-flex items-center gap-1 text-[11px] font-medium bg-status-warn-bg text-status-warn px-1.5 py-0.5 rounded"
						data-testid="badge-pending"
						title="Pending transaction"
					>
						<svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
							<circle cx="12" cy="12" r="10" />
							<polyline points="12 6 12 12 16 14" />
						</svg>
						Pending
					</span>
				{:else if isNew}
					<span
						class="inline-flex items-center text-[11px] font-medium bg-accent-tint text-accent-pop px-1.5 py-0.5 rounded"
						data-testid="badge-new"
						title="Needs categorization"
					>
						New
					</span>
				{/if}

				{#if isOfflineQueued}
					<span
						class="inline-flex items-center gap-1 text-[11px] font-medium bg-accent-tint text-accent-pop border border-accent-pop/30 px-1.5 py-0.5 rounded"
						data-testid="badge-offline-queued"
						title="Queued offline for sync"
					>
						<svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
							<path d="M12 16V8" />
							<path d="m9 11 3-3 3 3" />
							<path d="M20 16.58A5 5 0 0 0 18 7h-1.26A8 8 0 1 0 4 15.25" />
						</svg>
						Queued
					</span>
				{/if}

				{#if hasSettledDelta}
					<span
						class="inline-flex items-center gap-1 text-[11px] font-medium bg-status-warn-bg text-status-warn border border-status-warn/30 px-1.5 py-0.5 rounded"
						data-testid="badge-settled-delta"
						title="Settled amount differs from authorization"
					>
						Δ {transaction.settledDelta! > 0 ? '+' : '-'}${Math.abs(transaction.settledDelta!).toFixed(2)}
					</span>
				{/if}
			</div>

			<div class="flex items-center gap-2 text-xs text-ink-secondary mt-0.5">
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
				? 'italic text-ink-secondary'
				: transaction.amount > 0
					? 'text-status-good'
					: 'text-ink-primary'}"
		>
			{formatCurrency(transaction.amount)}
		</span>
	</div>
</div>
