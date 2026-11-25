<script lang="ts">
	import type { CategoryBudget } from '$lib/types/budget';
	import type {
		AccountItem,
		TransactionItemModel,
		UpdateTransactionPayload
	} from '$lib/types/transaction';

	interface Props {
		transaction: TransactionItemModel;
		account: AccountItem | null;
		categories: CategoryBudget[];
		variant?: 'panel' | 'modal';
		onClose: () => void;
		onUpdateTransaction?: (txId: string, payload: UpdateTransactionPayload) => Promise<void>;
		onDeleteTransaction?: (txId: string) => Promise<void>;
	}

	let {
		transaction,
		account,
		categories,
		variant = 'panel',
		onClose,
		onUpdateTransaction,
		onDeleteTransaction
	}: Props = $props();

	let editMerchant = $state('');
	let editNotes = $state('');
	let editCategoryId = $state('');
	let saving = $state(false);
	let deleting = $state(false);
	let confirmDelete = $state(false);
	let errorMsg = $state<string | null>(null);
	let lastHydratedKey = $state('');

	let isQueued = $derived(
		transaction.origin === 'MANUAL_OFFLINE' && transaction.syncStatus === 'QUEUED'
	);
	let hasSplits = $derived(Boolean(transaction.splits && transaction.splits.length > 0));
	let isOffBudget = $derived(account ? !account.isOnBudget : false);
	let canCategorize = $derived(!isQueued && !transaction.isTransfer && !isOffBudget && !hasSplits);
	let displayMerchant = $derived(transaction.merchantClean?.trim() || transaction.merchantRaw);
	let showOriginalMerchant = $derived(
		Boolean(
			transaction.merchantClean?.trim() &&
				transaction.merchantClean.trim() !== transaction.merchantRaw
		)
	);

	let categoryById = $derived(new Map(categories.map((c) => [c.id, c])));
	let groupedCategories = $derived.by(() => {
		const groups = new Map<string, CategoryBudget[]>();
		for (const cat of categories) {
			const list = groups.get(cat.groupName) ?? [];
			list.push(cat);
			groups.set(cat.groupName, list);
		}
		return Array.from(groups.entries());
	});

	$effect(() => {
		const key = `${transaction.id}|${transaction.merchantClean ?? ''}|${transaction.userNotes ?? ''}|${transaction.categoryId ?? ''}`;
		if (key !== lastHydratedKey) {
			lastHydratedKey = key;
			editMerchant = transaction.merchantClean?.trim() || transaction.merchantRaw;
			editNotes = transaction.userNotes ?? '';
			editCategoryId = transaction.categoryId ?? '';
			confirmDelete = false;
			errorMsg = null;
		}
	});

	function formatCurrency(val: number): string {
		const abs = Math.abs(val).toLocaleString('en-US', {
			minimumFractionDigits: 2,
			maximumFractionDigits: 2
		});
		if (val > 0) return `+$${abs}`;
		if (val < 0) return `-$${abs}`;
		return `$${abs}`;
	}

	function formatDate(isoDate: string): string {
		const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(isoDate);
		if (!m) return isoDate;
		const d = new Date(Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3])));
		if (Number.isNaN(d.getTime())) return isoDate;
		return d.toLocaleDateString('en-US', {
			month: 'short',
			day: 'numeric',
			year: 'numeric',
			timeZone: 'UTC'
		});
	}

	function originLabel(origin: TransactionItemModel['origin']): string {
		if (origin === 'BANK_FEED') return 'Bank Sync';
		if (origin === 'RECURRING') return 'Recurring';
		return 'Manual';
	}

	async function handleSave(e: SubmitEvent) {
		e.preventDefault();
		const trimmed = editMerchant.trim();
		if (!trimmed || !onUpdateTransaction) return;

		saving = true;
		errorMsg = null;
		try {
			const payload: UpdateTransactionPayload = {
				merchantClean: trimmed,
				userNotes: editNotes.trim()
			};
			if (canCategorize) {
				if (editCategoryId) {
					payload.categoryId = editCategoryId;
				} else {
					payload.clearCategory = true;
				}
			}
			await onUpdateTransaction(transaction.id, payload);
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to update transaction.';
		} finally {
			saving = false;
		}
	}

	async function handleDeleteConfirmed() {
		if (!onDeleteTransaction) return;
		deleting = true;
		errorMsg = null;
		try {
			await onDeleteTransaction(transaction.id);
			onClose();
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to delete transaction.';
		} finally {
			deleting = false;
		}
	}
</script>

<div class="flex flex-col h-full min-h-0 bg-surface text-ink-primary" data-testid="transaction-detail-card">
	<!-- Header -->
	<div class="px-4 py-3 border-b border-border bg-subtle flex items-center justify-between gap-2 shrink-0">
		{#if variant === 'panel'}
			<button
				type="button"
				data-testid="tx-detail-close"
				onclick={onClose}
				class="px-2 py-1 -ml-2 rounded-lg text-xs font-semibold text-accent-pop hover:bg-surface-hover transition-colors"
			>
				← All Transactions
			</button>
			<span class="text-xs font-bold uppercase tracking-wider text-ink-secondary">
				Transaction Details
			</span>
		{:else}
			<h2 class="text-sm font-bold text-ink-primary">Transaction Details</h2>
			<button
				type="button"
				data-testid="tx-detail-close"
				aria-label="Close transaction details"
				onclick={onClose}
				class="p-1.5 rounded-lg text-ink-secondary hover:text-ink-primary hover:bg-surface-hover transition-colors"
			>
				✕
			</button>
		{/if}
	</div>

	<!-- Body -->
	<div class="flex-1 overflow-y-auto overscroll-contain-scroll p-4 space-y-4">
		<!-- Summary Hero -->
		<div class="rounded-xl border border-border bg-subtle/60 p-3.5 space-y-2">
			<div class="flex items-start justify-between gap-3">
				<div class="min-w-0">
					<h3 class="text-base font-bold text-ink-primary break-words">{displayMerchant}</h3>
					{#if showOriginalMerchant}
						<p class="text-[11px] text-ink-muted mt-0.5 break-words">
							Original: {transaction.merchantRaw}
						</p>
					{/if}
				</div>
				<span
					class="text-lg font-mono font-bold shrink-0 {transaction.amount > 0
						? 'text-status-good'
						: 'text-ink-primary'}"
				>
					{formatCurrency(transaction.amount)}
				</span>
			</div>

			<div class="flex flex-wrap items-center gap-1.5">
				{#if isQueued}
					<span class="inline-flex items-center rounded-full bg-accent-tint text-accent-pop border border-accent-pop/30 px-2 py-0.5 text-[11px] font-medium">
						Queued offline
					</span>
				{:else if transaction.status === 'PENDING'}
					<span class="inline-flex items-center rounded-full bg-status-warn-bg text-status-warn px-2 py-0.5 text-[11px] font-medium">
						Pending
					</span>
				{:else}
					<span class="inline-flex items-center rounded-full bg-status-good-bg text-status-good px-2 py-0.5 text-[11px] font-medium">
						Posted
					</span>
				{/if}

				{#if transaction.isTransfer}
					<span class="inline-flex items-center rounded-full border border-border bg-surface px-2 py-0.5 text-[11px] text-ink-secondary">
						Transfer
					</span>
				{/if}

				{#if isOffBudget}
					<span class="inline-flex items-center rounded-full border border-border bg-surface px-2 py-0.5 text-[11px] text-ink-secondary">
						Off-Budget
					</span>
				{/if}

				{#if transaction.settledDelta != null && transaction.settledDelta !== 0}
					<span class="inline-flex items-center rounded-full bg-status-warn-bg text-status-warn border border-status-warn/30 px-2 py-0.5 text-[11px] font-medium">
						Settled Δ {transaction.settledDelta > 0 ? '+' : '-'}${Math.abs(transaction.settledDelta).toFixed(2)}
					</span>
				{/if}
			</div>
		</div>

		<!-- Metadata -->
		<dl class="grid grid-cols-2 gap-3 text-xs rounded-xl border border-border bg-surface p-3.5">
			<div>
				<dt class="text-[11px] text-ink-muted">Date</dt>
				<dd class="font-medium text-ink-primary mt-0.5">{formatDate(transaction.transactionDate)}</dd>
			</div>
			<div>
				<dt class="text-[11px] text-ink-muted">Source</dt>
				<dd class="font-medium text-ink-primary mt-0.5">{originLabel(transaction.origin)}</dd>
			</div>
			<div class="col-span-2">
				<dt class="text-[11px] text-ink-muted">Account</dt>
				<dd class="font-medium text-ink-primary mt-0.5 flex items-center gap-1.5 flex-wrap">
					<span>{account?.name || transaction.accountName || 'Account'}</span>
					{#if account}
						<span class="text-[10px] px-1.5 py-0.5 rounded bg-subtle border border-border text-ink-secondary">
							{account.isOnBudget ? 'On-Budget' : 'Off-Budget'}
						</span>
					{/if}
				</dd>
			</div>
			<div class="col-span-2">
				<dt class="text-[11px] text-ink-muted">Category</dt>
				<dd class="font-medium text-ink-primary mt-0.5">
					{#if hasSplits && transaction.splits}
						<ul class="space-y-1 mt-1">
							{#each transaction.splits as split (split.id)}
								{@const splitCat = split.categoryId ? categoryById.get(split.categoryId) : null}
								<li class="flex items-center justify-between text-xs bg-subtle rounded px-2 py-1">
									<span>{splitCat ? `${splitCat.groupName} — ${splitCat.name}` : 'Uncategorized'}</span>
									<span class="font-mono">{formatCurrency(split.amount)}</span>
								</li>
							{/each}
						</ul>
					{:else if transaction.categoryId}
						{@const cat = categoryById.get(transaction.categoryId)}
						<span>{cat ? `${cat.groupName} — ${cat.name}` : transaction.categoryName ?? 'Categorized'}</span>
					{:else}
						<span class="text-ink-muted">Uncategorized</span>
					{/if}
				</dd>
			</div>
		</dl>

		{#if errorMsg}
			<p
				role="alert"
				class="text-xs text-status-danger bg-status-danger-bg border border-status-danger/30 rounded-lg px-3 py-2"
			>
				{errorMsg}
			</p>
		{/if}

		<!-- Edit / Delete Controls -->
		{#if !isQueued && (onUpdateTransaction || onDeleteTransaction)}
			{#if onUpdateTransaction}
				<form onsubmit={handleSave} data-testid="tx-detail-edit-form" class="space-y-3 rounded-xl border border-border bg-subtle/40 p-3.5">
					<p class="text-xs font-semibold text-ink-primary">Edit Details</p>

					<div>
						<label for="tx-detail-merchant" class="block text-[11px] font-medium text-ink-secondary mb-1">
							Merchant
						</label>
						<input
							id="tx-detail-merchant"
							type="text"
							required
							bind:value={editMerchant}
							class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-xs text-ink-primary focus:border-accent-pop focus:outline-none"
						/>
					</div>

					{#if canCategorize}
						<div>
							<label for="tx-detail-category" class="block text-[11px] font-medium text-ink-secondary mb-1">
								Category
							</label>
							<select
								id="tx-detail-category"
								bind:value={editCategoryId}
								class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-xs text-ink-primary focus:border-accent-pop focus:outline-none"
							>
								<option value="">Uncategorized</option>
								{#each groupedCategories as [grp, cats] (grp)}
									<optgroup label={grp}>
										{#each cats as cat (cat.id)}
											<option value={cat.id}>{cat.name}</option>
										{/each}
									</optgroup>
								{/each}
							</select>
						</div>
					{/if}

					<div>
						<label for="tx-detail-notes" class="block text-[11px] font-medium text-ink-secondary mb-1">
							Notes
						</label>
						<input
							id="tx-detail-notes"
							type="text"
							placeholder="Optional note..."
							bind:value={editNotes}
							class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
						/>
					</div>

					<div class="flex justify-end pt-1">
						<button
							type="submit"
							disabled={saving || deleting || !editMerchant.trim()}
							class="px-4 py-2 rounded-lg bg-accent-primary text-accent-primary-text text-xs font-semibold hover:opacity-90 disabled:opacity-50 transition-opacity"
						>
							{saving ? 'Saving...' : 'Save Changes'}
						</button>
					</div>
				</form>
			{/if}

			{#if onDeleteTransaction}
				<div class="pt-1">
					{#if confirmDelete}
						<div class="rounded-xl border border-status-danger/40 bg-status-danger-bg p-3 space-y-2">
							<p class="text-xs text-status-danger font-medium">
								Delete this transaction? This cannot be undone.
							</p>
							<div class="flex items-center justify-end gap-2">
								<button
									type="button"
									disabled={deleting}
									onclick={() => (confirmDelete = false)}
									class="px-3 py-1.5 rounded-lg border border-border bg-surface text-xs text-ink-secondary hover:text-ink-primary"
								>
									Cancel
								</button>
								<button
									type="button"
									data-testid="tx-detail-confirm-delete"
									disabled={deleting}
									onclick={() => void handleDeleteConfirmed()}
									class="px-3 py-1.5 rounded-lg bg-status-danger text-accent-primary-text text-xs font-semibold hover:opacity-90 disabled:opacity-50"
								>
									{deleting ? 'Deleting...' : 'Confirm Delete'}
								</button>
							</div>
						</div>
					{:else}
						<button
							type="button"
							data-testid="tx-detail-delete-btn"
							onclick={() => (confirmDelete = true)}
							class="w-full px-3 py-2 rounded-lg border border-status-danger/40 text-xs font-medium text-status-danger hover:bg-status-danger-bg transition-colors"
						>
							Delete Transaction
						</button>
					{/if}
				</div>
			{/if}
		{/if}
	</div>
</div>
