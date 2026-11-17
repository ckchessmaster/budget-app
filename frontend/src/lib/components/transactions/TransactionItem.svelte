<script lang="ts">
	import type { TransactionView } from '$lib/types/transactions';

	interface Props {
		transaction: TransactionView;
	}

	let { transaction }: Props = $props();

	let isPending = $derived(transaction.status === 'PENDING');
	let isQueuedOffline = $derived(
		transaction.origin === 'MANUAL_OFFLINE' && transaction.syncStatus === 'QUEUED'
	);
	let hasSettledDelta = $derived(
		transaction.settledDelta != null && Math.abs(transaction.settledDelta) > 0.005
	);

	function fmtAmount(n: number): string {
		const abs = Math.abs(n).toLocaleString('en-US', {
			minimumFractionDigits: 2,
			maximumFractionDigits: 2
		});
		return n < 0 ? `-$${abs}` : `+$${abs}`;
	}
</script>

<div
	data-testid="transaction-item"
	class="flex items-center justify-between gap-3 rounded-xl border border-slate-800/80 bg-slate-900/70 px-4 py-3 {isPending
		? 'text-slate-400 italic'
		: 'text-slate-100'}"
>
	<div class="min-w-0 flex-1">
		<div class="flex items-center gap-2">
			{#if isPending}
				<!-- Clock icon for PENDING -->
				<svg
					data-testid="badge-pending"
					class="h-4 w-4 shrink-0 text-slate-400"
					viewBox="0 0 24 24"
					fill="none"
					stroke="currentColor"
					stroke-width="2"
					aria-label="Pending transaction"
				>
					<circle cx="12" cy="12" r="10" />
					<polyline points="12 6 12 12 16 14" />
				</svg>
			{/if}

			{#if isQueuedOffline}
				<!-- Cloud upload icon for MANUAL_OFFLINE + QUEUED -->
				<svg
					data-testid="badge-queued-offline"
					class="h-4 w-4 shrink-0 text-sky-400"
					viewBox="0 0 24 24"
					fill="none"
					stroke="currentColor"
					stroke-width="2"
					aria-label="Queued offline"
				>
					<path d="M12 13v8" />
					<path d="m8 17 4-4 4 4" />
					<path d="M4 14.899A7 7 0 1 1 15.71 8h1.79a4.5 4.5 0 0 1 2.5 8.242" />
				</svg>
			{/if}

			<span class="truncate text-sm font-medium">
				{transaction.merchantClean || transaction.merchantRaw}
			</span>

			{#if hasSettledDelta}
				<span
					data-testid="badge-settled-delta"
					class="inline-flex items-center rounded-md bg-amber-500/15 px-1.5 py-0.5 text-[11px] font-semibold text-amber-300 border border-amber-500/30 not-italic"
				>
					Δ {fmtAmount(transaction.settledDelta ?? 0)}
				</span>
			{/if}
		</div>

		<div class="mt-0.5 flex items-center gap-2 text-xs text-slate-500 not-italic">
			<span>{transaction.transactionDate}</span>
			{#if transaction.categoryName}
				<span>•</span>
				<span>{transaction.categoryName}</span>
			{/if}
			{#if transaction.accountName}
				<span>•</span>
				<span>{transaction.accountName}</span>
			{/if}
		</div>
	</div>

	<div
		class="shrink-0 text-sm font-semibold tabular-nums {transaction.amount >= 0
			? 'text-emerald-400'
			: isPending
				? 'text-slate-400'
				: 'text-slate-100'}"
	>
		{fmtAmount(transaction.amount)}
	</div>
</div>
