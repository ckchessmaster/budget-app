<script lang="ts">
	import type { InlineActivityItem } from '$lib/types/budget';

	interface Props {
		transactions: InlineActivityItem[];
		embedded?: boolean;
		selectedTransactionId?: string | null;
		onSelectTransaction?: (txId: string) => void;
	}

	let {
		transactions,
		embedded = true,
		selectedTransactionId = null,
		onSelectTransaction
	}: Props = $props();

	let sortedTransactions = $derived(
		[...transactions].sort((a, b) => b.date.localeCompare(a.date))
	);

	function fmtShortDate(isoDate: string): string {
		const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(isoDate);
		if (!m) return isoDate;
		const year = Number(m[1]);
		const month = Number(m[2]);
		const day = Number(m[3]);
		const d = new Date(Date.UTC(year, month - 1, day));
		if (Number.isNaN(d.getTime())) return isoDate;
		return d.toLocaleDateString('en-US', {
			month: 'short',
			day: 'numeric',
			timeZone: 'UTC'
		});
	}

	function fmtAmount(amount: number): string {
		const abs = Math.abs(amount).toLocaleString('en-US', {
			minimumFractionDigits: 2,
			maximumFractionDigits: 2
		});
		if (amount > 0) return `+$${abs}`;
		if (amount < 0) return `-$${abs}`;
		return `$${abs}`;
	}
</script>

<div
	data-testid="inline-activity-drawer"
	class={embedded
		? 'bg-subtle/70 border-t border-border px-2.5 sm:px-4 py-2.5'
		: 'mt-2.5 pt-2.5 border-t border-border'}
>
	{#if sortedTransactions.length === 0}
		<p class="text-xs text-ink-muted px-2 py-1">No tracked transactions yet.</p>
	{:else}
		<ul class="space-y-1">
			{#each sortedTransactions as tx (tx.id)}
				{@const targetTxId = tx.transactionId ?? tx.id}
				{@const isSelected = selectedTransactionId === targetTxId}
				<li data-testid="inline-activity-row">
					{#if onSelectTransaction}
						<button
							type="button"
							data-testid="inline-activity-btn"
							aria-pressed={isSelected}
							onclick={(e) => {
								e.stopPropagation();
								onSelectTransaction(targetTxId);
							}}
							class="w-full text-left flex items-center justify-between gap-3 px-2.5 py-2.5 sm:py-2 min-h-[44px] sm:min-h-[38px] rounded-lg text-xs sm:text-[13px] transition-colors cursor-pointer {isSelected
								? 'bg-accent-tint ring-1 ring-accent-pop/50'
								: 'hover:bg-surface-hover/80 active:bg-surface-hover'}"
						>
							<div class="min-w-0 flex items-center gap-2 truncate">
								<span class="font-medium text-ink-primary truncate">
									{fmtShortDate(tx.date)} · {tx.merchant}
								</span>
								<span class="text-ink-muted max-w-[38%] truncate">({tx.accountName})</span>
								{#if tx.status === 'PENDING'}
									<span class="inline-flex items-center shrink-0 rounded-full bg-status-warn-bg text-status-warn px-1.5 py-0.5 text-[10px] font-medium">
										Pending
									</span>
								{/if}
							</div>
							<div class="flex items-center gap-1.5 shrink-0">
								<span
									class="font-mono font-semibold {tx.amount > 0
										? 'text-status-good'
										: tx.amount < 0
											? 'text-ink-primary'
											: 'text-ink-muted'}"
								>
									{fmtAmount(tx.amount)}
								</span>
								<svg
									class="w-3.5 h-3.5 text-ink-muted shrink-0"
									viewBox="0 0 24 24"
									fill="none"
									stroke="currentColor"
									stroke-width="2"
									aria-hidden="true"
								>
									<path d="m9 18 6-6-6-6" />
								</svg>
							</div>
						</button>
					{:else}
						<div class="flex items-center justify-between gap-3 px-2.5 py-2.5 sm:py-2 min-h-[44px] sm:min-h-[38px] rounded-lg text-xs sm:text-[13px]">
							<div class="min-w-0 flex items-center gap-2 truncate">
								<span class="font-medium text-ink-primary truncate">
									{fmtShortDate(tx.date)} · {tx.merchant}
								</span>
								<span class="text-ink-muted max-w-[38%] truncate">({tx.accountName})</span>
								{#if tx.status === 'PENDING'}
									<span class="inline-flex items-center shrink-0 rounded-full bg-status-warn-bg text-status-warn px-1.5 py-0.5 text-[10px] font-medium">
										Pending
									</span>
								{/if}
							</div>
							<span
								class="font-mono font-semibold shrink-0 {tx.amount > 0
									? 'text-status-good'
									: tx.amount < 0
										? 'text-ink-primary'
										: 'text-ink-muted'}"
							>
								{fmtAmount(tx.amount)}
							</span>
						</div>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}
</div>
