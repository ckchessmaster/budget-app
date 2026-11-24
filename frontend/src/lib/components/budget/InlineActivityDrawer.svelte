<script lang="ts">
	import type { InlineActivityItem } from '$lib/types/budget';

	interface Props {
		transactions: InlineActivityItem[];
		embedded?: boolean;
	}

	let { transactions, embedded = true }: Props = $props();

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
		? 'bg-subtle/70 border-t border-border px-5 py-2.5'
		: 'mt-2.5 pt-2.5 border-t border-border'}
>
	{#if sortedTransactions.length === 0}
		<p class="text-xs text-ink-muted">No tracked transactions yet.</p>
	{:else}
		<ul class="space-y-1.5">
			{#each sortedTransactions as tx (tx.id)}
				<li
					data-testid="inline-activity-row"
					class="flex items-center justify-between gap-3 text-xs"
				>
					<div class="min-w-0 flex items-center gap-1.5 truncate">
						<span class="text-ink-secondary truncate">
							{fmtShortDate(tx.date)} · {tx.merchant}
						</span>
						<span class="text-ink-muted shrink-0">({tx.accountName})</span>
						{#if tx.status === 'PENDING'}
							<span class="inline-flex items-center shrink-0 rounded-full bg-status-warn-bg text-status-warn px-1.5 py-0.5 text-[10px] font-medium">
								Pending
							</span>
						{/if}
					</div>
					<span
						class="font-mono font-medium shrink-0 {tx.amount > 0
							? 'text-status-good'
							: tx.amount < 0
								? 'text-ink-primary'
								: 'text-ink-muted'}"
					>
						{fmtAmount(tx.amount)}
					</span>
				</li>
			{/each}
		</ul>
	{/if}
</div>
