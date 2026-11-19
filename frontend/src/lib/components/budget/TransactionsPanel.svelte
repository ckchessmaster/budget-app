<script lang="ts">
	import type { OfflineTransaction } from '$lib/offline/db';
	import type { CategoryBudget } from '$lib/types/budget';
	import type { AccountItem, TransactionItemModel } from '$lib/types/transaction';

	type TxSubTab = 'new' | 'tracked' | 'pending' | 'all';

	interface Props {
		currentMonth: string;
		transactions: TransactionItemModel[];
		queuedTransactions: OfflineTransaction[];
		accounts: AccountItem[];
		categories: CategoryBudget[];
		isOffline: boolean;
		syncing: boolean;
		activeCategoryFilterId: string | null;
		onClearCategoryFilter: () => void;
		onCreateTransaction: (payload: {
			accountId: string;
			amount: number;
			merchantRaw: string;
			categoryId: string | null;
			transactionDate: string;
		}) => Promise<void>;
		onCategorizeTransaction: (txId: string, categoryId: string | null) => Promise<void>;
		onDeleteTransaction: (txId: string) => Promise<void>;
		onSyncNow: () => Promise<void>;
	}

	let {
		currentMonth,
		transactions,
		queuedTransactions,
		accounts,
		categories,
		isOffline,
		syncing,
		activeCategoryFilterId,
		onClearCategoryFilter,
		onCreateTransaction,
		onCategorizeTransaction,
		onDeleteTransaction,
		onSyncNow
	}: Props = $props();

	let activeSubTab = $state<TxSubTab>('new');
	let searchQuery = $state('');
	let selectedAccountId = $state('');
	let showQuickAdd = $state(false);
	let savingIds = $state<Record<string, boolean>>({});
	let confirmDeleteId = $state<string | null>(null);
	let actionError = $state<string | null>(null);

	// Quick-Add Form State
	let formAccountId = $state('');
	let formDirection = $state<'expense' | 'inflow'>('expense');
	let formCategoryId = $state('');
	let formMerchant = $state('');
	let formAmount = $state('');
	let formDate = $state('');
	let formDateDirty = $state(false);
	let submitting = $state(false);

	function defaultDateForMonth(ym: string): string {
		const today = new Date().toISOString().slice(0, 10);
		return today.startsWith(ym) ? today : `${ym}-01`;
	}

	$effect(() => {
		if (!formAccountId && accounts.length > 0) {
			formAccountId = accounts[0].id;
		}
	});

	$effect(() => {
		if (!formDateDirty) {
			formDate = defaultDateForMonth(currentMonth);
		}
	});

	$effect(() => {
		// When user filters by a specific category from the left budget list, ensure All/Tracked can show it
		if (activeCategoryFilterId && activeSubTab === 'new') {
			activeSubTab = 'all';
		}
	});

	let selectedFormAccount = $derived(accounts.find((a) => a.id === formAccountId) ?? null);
	let isOffBudgetFormAccount = $derived(
		selectedFormAccount !== null && !selectedFormAccount.isOnBudget
	);

	$effect(() => {
		if (isOffBudgetFormAccount && formCategoryId !== '') {
			formCategoryId = '';
		}
	});

	let accountById = $derived(new Map(accounts.map((a) => [a.id, a])));
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

	let combinedItems = $derived.by(() => {
		const offlineMapped: TransactionItemModel[] = queuedTransactions
			.filter((q) => q.transactionDate.startsWith(currentMonth))
			.map((q) => ({
				id: q.localId,
				accountId: q.accountId,
				origin: 'MANUAL_OFFLINE',
				status: 'PENDING',
				amount: q.amount,
				merchantRaw: q.merchantRaw,
				categoryId: q.categoryId,
				transactionDate: q.transactionDate,
				isCleared: false,
				isTransfer: false,
				syncStatus: q.syncStatus
			}));
		return [...offlineMapped, ...transactions];
	});

	function isNewUncategorized(tx: TransactionItemModel): boolean {
		if (tx.categoryId !== null) return false;
		if (tx.isTransfer) return false;
		if (tx.splits && tx.splits.length > 0) return false;
		const acct = accountById.get(tx.accountId);
		if (acct && !acct.isOnBudget) return false;
		return true;
	}

	let newItems = $derived(combinedItems.filter(isNewUncategorized));
	let pendingItems = $derived(
		combinedItems.filter((tx) => tx.status === 'PENDING' || tx.syncStatus === 'QUEUED')
	);

	let filteredItems = $derived.by(() => {
		let base = combinedItems;
		if (activeSubTab === 'new') {
			base = newItems;
		} else if (activeSubTab === 'tracked') {
			base = combinedItems.filter((tx) => !isNewUncategorized(tx));
		} else if (activeSubTab === 'pending') {
			base = pendingItems;
		}

		if (selectedAccountId) {
			base = base.filter((tx) => tx.accountId === selectedAccountId);
		}

		if (activeCategoryFilterId) {
			base = base.filter(
				(tx) =>
					tx.categoryId === activeCategoryFilterId ||
					tx.splits?.some((s) => s.categoryId === activeCategoryFilterId)
			);
		}

		const q = searchQuery.trim().toLowerCase();
		if (q) {
			base = base.filter((tx) => {
				const merchant = (tx.merchantClean || tx.merchantRaw).toLowerCase();
				const acct = (accountById.get(tx.accountId)?.name || '').toLowerCase();
				const cat = tx.categoryId ? (categoryById.get(tx.categoryId)?.name || '').toLowerCase() : '';
				const notes = (tx.userNotes || '').toLowerCase();
				const amt = Math.abs(tx.amount).toFixed(2);
				return (
					merchant.includes(q) ||
					acct.includes(q) ||
					cat.includes(q) ||
					notes.includes(q) ||
					amt.includes(q)
				);
			});
		}

		return base;
	});

	const SUB_TABS: Array<{ id: TxSubTab; label: (newCount: number, pendingCount: number) => string }> = [
		{ id: 'new', label: (n) => `New (${n})` },
		{ id: 'tracked', label: () => 'Tracked' },
		{ id: 'pending', label: (_n, p) => (p > 0 ? `Pending (${p})` : 'Pending') },
		{ id: 'all', label: () => 'All' }
	];

	function parseDateBadge(isoDate: string): { month: string; day: string } {
		const [y, m, d] = isoDate.slice(0, 10).split('-').map(Number);
		if (!y || !m || !d) return { month: '', day: isoDate.slice(0, 2) };
		const month = new Date(Date.UTC(y, m - 1, d)).toLocaleDateString('en-US', {
			month: 'short',
			timeZone: 'UTC'
		});
		return { month, day: String(d) };
	}

	function formatCurrency(val: number): string {
		const abs = Math.abs(val).toLocaleString('en-US', {
			minimumFractionDigits: 2,
			maximumFractionDigits: 2
		});
		return val < 0 ? `-$${abs}` : `+$${abs}`;
	}

	async function handleCategorySelect(txId: string, rawVal: string) {
		const nextCatId = rawVal === '' ? null : rawVal;
		savingIds = { ...savingIds, [txId]: true };
		actionError = null;
		try {
			await onCategorizeTransaction(txId, nextCatId);
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to categorize transaction.';
		} finally {
			const next = { ...savingIds };
			delete next[txId];
			savingIds = next;
		}
	}

	async function handleQuickAddSubmit(e: SubmitEvent) {
		e.preventDefault();
		const parsedAmount = Number(formAmount);
		if (!formAccountId || !formMerchant.trim() || !Number.isFinite(parsedAmount) || parsedAmount === 0) {
			return;
		}
		submitting = true;
		actionError = null;
		const absAmount = Math.abs(parsedAmount);
		const normalizedAmount = formDirection === 'inflow' ? absAmount : -absAmount;
		const effectiveCategoryId = isOffBudgetFormAccount ? null : formCategoryId || null;

		try {
			await onCreateTransaction({
				accountId: formAccountId,
				amount: normalizedAmount,
				merchantRaw: formMerchant.trim(),
				categoryId: effectiveCategoryId,
				transactionDate: formDate || defaultDateForMonth(currentMonth)
			});
			formMerchant = '';
			formAmount = '';
			formCategoryId = '';
			showQuickAdd = false;
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to record transaction.';
		} finally {
			submitting = false;
		}
	}

	async function handleDelete(txId: string) {
		savingIds = { ...savingIds, [txId]: true };
		actionError = null;
		try {
			await onDeleteTransaction(txId);
			confirmDeleteId = null;
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to delete transaction.';
		} finally {
			const next = { ...savingIds };
			delete next[txId];
			savingIds = next;
		}
	}

	function handleSubTabKeydown(e: KeyboardEvent, idx: number) {
		if (e.key === 'ArrowRight') {
			e.preventDefault();
			activeSubTab = SUB_TABS[(idx + 1) % SUB_TABS.length].id;
		} else if (e.key === 'ArrowLeft') {
			e.preventDefault();
			activeSubTab = SUB_TABS[(idx - 1 + SUB_TABS.length) % SUB_TABS.length].id;
		}
	}
</script>

<div class="flex flex-col h-full min-h-0" data-testid="transactions-panel">
	<!-- Sub-Tabs -->
	<div
		role="tablist"
		aria-label="Transaction status filter"
		class="grid grid-cols-4 border-b border-slate-200 dark:border-slate-800 px-2 shrink-0"
	>
		{#each SUB_TABS as tab, idx}
			{@const isSelected = activeSubTab === tab.id}
			<button
				type="button"
				role="tab"
				aria-selected={isSelected}
				tabindex={isSelected ? 0 : -1}
				onclick={() => (activeSubTab = tab.id)}
				onkeydown={(e) => handleSubTabKeydown(e, idx)}
				class="relative py-2.5 text-xs font-semibold transition-colors {isSelected
					? 'text-sky-600 dark:text-sky-400 after:content-[""] after:absolute after:inset-x-2 after:bottom-0 after:h-0.5 after:bg-sky-600 dark:after:bg-sky-400'
					: 'text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200'}"
			>
				{tab.label(newItems.length, pendingItems.length)}
			</button>
		{/each}
	</div>

	<!-- Search + Filter + Quick-Add Trigger -->
	<div class="p-3 space-y-2 border-b border-slate-100 dark:border-slate-800/80 shrink-0">
		<div class="flex items-center gap-2">
			<div class="relative flex-1">
				<svg
					class="w-3.5 h-3.5 text-slate-400 absolute left-2.5 top-1/2 -translate-y-1/2 pointer-events-none"
					viewBox="0 0 24 24"
					fill="none"
					stroke="currentColor"
					stroke-width="2"
					aria-hidden="true"
				>
					<circle cx="11" cy="11" r="8" />
					<path d="m21 21-4.3-4.3" />
				</svg>
				<input
					type="search"
					placeholder="Search merchant, account, amount..."
					aria-label="Search transactions"
					bind:value={searchQuery}
					class="w-full rounded-lg border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-950 pl-8 pr-3 py-1.5 text-xs text-slate-900 dark:text-slate-100 placeholder:text-slate-400 focus:border-sky-500 focus:outline-none"
				/>
			</div>

			<button
				type="button"
				onclick={() => (showQuickAdd = !showQuickAdd)}
				aria-expanded={showQuickAdd}
				class="shrink-0 rounded-lg px-2.5 py-1.5 text-xs font-semibold transition-colors {showQuickAdd
					? 'bg-slate-200 dark:bg-slate-800 text-slate-800 dark:text-slate-200'
					: 'bg-emerald-600 hover:bg-emerald-500 text-white'}"
			>
				{showQuickAdd ? 'Close' : '+ Add'}
			</button>
		</div>

		<div class="flex flex-wrap items-center gap-2">
			<select
				aria-label="Filter by account"
				bind:value={selectedAccountId}
				class="rounded-md border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-950 px-2 py-1 text-[11px] text-slate-700 dark:text-slate-200 focus:outline-none"
			>
				<option value="">All Accounts</option>
				{#each accounts as acct}
					<option value={acct.id}>{acct.name}</option>
				{/each}
			</select>

			{#if activeCategoryFilterId}
				{@const filteredCat = categoryById.get(activeCategoryFilterId)}
				<button
					type="button"
					onclick={onClearCategoryFilter}
					class="inline-flex items-center gap-1 rounded-full bg-sky-50 dark:bg-sky-500/15 border border-sky-200 dark:border-sky-500/30 px-2 py-0.5 text-[11px] font-medium text-sky-700 dark:text-sky-300"
				>
					<span>Category: {filteredCat?.name ?? 'Selected'}</span>
					<span aria-hidden="true">✕</span>
				</button>
			{/if}

			{#if queuedTransactions.length > 0}
				<button
					type="button"
					onclick={() => void onSyncNow()}
					disabled={syncing || isOffline}
					class="ml-auto rounded-md bg-sky-600 hover:bg-sky-500 disabled:opacity-50 px-2 py-0.5 text-[11px] font-medium text-white"
				>
					{syncing ? 'Syncing...' : `Sync (${queuedTransactions.length})`}
				</button>
			{/if}
		</div>

		{#if showQuickAdd}
			<form
				onsubmit={handleQuickAddSubmit}
				data-testid="quick-add-tx-form"
				class="rounded-xl border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-950/70 p-3 space-y-2.5"
			>
				<div class="grid grid-cols-2 gap-2">
					<div>
						<label class="block text-[11px] text-slate-500 dark:text-slate-400 mb-0.5" for="qa-account">
							Account
						</label>
						<select
							id="qa-account"
							bind:value={formAccountId}
							required
							class="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-900 px-2 py-1 text-xs text-slate-900 dark:text-slate-100"
						>
							{#each accounts as acct}
								<option value={acct.id}>{acct.name}</option>
							{/each}
						</select>
					</div>

					<div>
						<span class="block text-[11px] text-slate-500 dark:text-slate-400 mb-0.5">Type</span>
						<div class="grid grid-cols-2 rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-900 p-0.5 text-[11px]">
							<button
								type="button"
								onclick={() => (formDirection = 'expense')}
								class="rounded py-0.5 font-medium transition-colors {formDirection === 'expense'
									? 'bg-rose-600 text-white'
									: 'text-slate-500 dark:text-slate-400'}"
							>
								Expense
							</button>
							<button
								type="button"
								onclick={() => (formDirection = 'inflow')}
								class="rounded py-0.5 font-medium transition-colors {formDirection === 'inflow'
									? 'bg-emerald-600 text-white'
									: 'text-slate-500 dark:text-slate-400'}"
							>
								Inflow
							</button>
						</div>
					</div>
				</div>

				<div class="grid grid-cols-3 gap-2">
					<div class="col-span-2">
						<label class="block text-[11px] text-slate-500 dark:text-slate-400 mb-0.5" for="qa-merchant">
							Merchant / Payee
						</label>
						<input
							id="qa-merchant"
							type="text"
							bind:value={formMerchant}
							placeholder={formDirection === 'inflow' ? 'Paycheck / Deposit' : 'Trader Joe\'s'}
							required
							class="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-900 px-2 py-1 text-xs text-slate-900 dark:text-slate-100"
						/>
					</div>

					<div>
						<label class="block text-[11px] text-slate-500 dark:text-slate-400 mb-0.5" for="qa-amount">
							Amount ($)
						</label>
						<input
							id="qa-amount"
							type="number"
							step="0.01"
							min="0.01"
							bind:value={formAmount}
							placeholder="25.00"
							required
							class="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-900 px-2 py-1 text-xs font-mono text-slate-900 dark:text-slate-100"
						/>
					</div>
				</div>

				<div class="grid grid-cols-2 gap-2">
					<div>
						<label class="block text-[11px] text-slate-500 dark:text-slate-400 mb-0.5" for="qa-date">
							Date
						</label>
						<input
							id="qa-date"
							type="date"
							bind:value={formDate}
							oninput={() => (formDateDirty = true)}
							required
							class="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-900 px-2 py-1 text-xs text-slate-900 dark:text-slate-100"
						/>
					</div>

					<div>
						<label class="block text-[11px] text-slate-500 dark:text-slate-400 mb-0.5" for="qa-category">
							Category
						</label>
						<select
							id="qa-category"
							bind:value={formCategoryId}
							disabled={isOffBudgetFormAccount}
							class="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-900 px-2 py-1 text-xs text-slate-900 dark:text-slate-100 disabled:opacity-50"
						>
							<option value="">
								{isOffBudgetFormAccount ? 'Off-budget' : 'Uncategorized'}
							</option>
							{#each groupedCategories as [grp, cats]}
								<optgroup label={grp}>
									{#each cats as cat}
										<option value={cat.id}>{cat.name}</option>
									{/each}
								</optgroup>
							{/each}
						</select>
					</div>
				</div>

				<div class="flex justify-end gap-2 pt-1">
					<button
						type="submit"
						disabled={submitting || !formAccountId}
						class="w-full rounded-lg bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 py-1.5 text-xs font-semibold text-white transition-colors"
					>
						{submitting
							? 'Saving...'
							: isOffline
								? 'Queue Offline Transaction'
								: formDirection === 'inflow'
									? 'Save Inflow'
									: 'Save Expense'}
					</button>
				</div>
			</form>
		{/if}

		{#if actionError}
			<p class="text-[11px] text-rose-600 dark:text-rose-400 bg-rose-50 dark:bg-rose-500/10 border border-rose-200 dark:border-rose-500/30 rounded-md px-2.5 py-1.5">
				{actionError}
			</p>
		{/if}
	</div>

	<!-- Transaction Feed -->
	<div class="flex-1 overflow-y-auto overscroll-contain-scroll divide-y divide-slate-100 dark:divide-slate-800/80">
		{#if filteredItems.length === 0}
			<div class="px-4 py-10 text-center space-y-1">
				<p class="text-xs font-medium text-slate-600 dark:text-slate-300">
					{#if activeSubTab === 'new'}
						All caught up — no uncategorized transactions!
					{:else}
						No matching transactions for {currentMonth}.
					{/if}
				</p>
				<p class="text-[11px] text-slate-400 dark:text-slate-500">
					Click "+ Add" above to record one manually.
				</p>
			</div>
		{:else}
			{#each filteredItems as tx (tx.id)}
				{@const badge = parseDateBadge(tx.transactionDate)}
				{@const acct = accountById.get(tx.accountId)}
				{@const isPending = tx.status === 'PENDING'}
				{@const isQueued = tx.origin === 'MANUAL_OFFLINE' && tx.syncStatus === 'QUEUED'}
				{@const hasSplits = Boolean(tx.splits && tx.splits.length > 0)}
				{@const isOffBudget = acct ? !acct.isOnBudget : false}
				{@const displayMerchant = tx.merchantClean || tx.merchantRaw}
				{@const isSaving = Boolean(savingIds[tx.id])}

				<div
					data-testid="tx-panel-row"
					class="group px-3.5 py-3 flex items-start justify-between gap-2.5 hover:bg-slate-50/70 dark:hover:bg-slate-800/30 transition-colors"
				>
					<!-- Circular Date Badge + Merchant + Account -->
					<div class="flex items-start gap-2.5 min-w-0 flex-1">
						<div
							class="w-9 h-9 rounded-full border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-950 flex flex-col items-center justify-center shrink-0 leading-none"
							aria-label={tx.transactionDate}
						>
							<span class="text-[9px] uppercase font-semibold text-sky-600 dark:text-sky-400">
								{badge.month}
							</span>
							<span class="text-xs font-bold text-slate-700 dark:text-slate-200 mt-0.5">
								{badge.day}
							</span>
						</div>

						<div class="min-w-0 flex-1">
							<div class="flex items-center gap-1.5 flex-wrap">
								<span
									class="text-xs font-semibold truncate {isPending
										? 'italic text-slate-500 dark:text-slate-400'
										: 'text-slate-900 dark:text-slate-100'}"
								>
									{displayMerchant}
								</span>

								{#if isPending}
									<span
										class="inline-flex items-center gap-0.5 text-[10px] italic text-slate-500 dark:text-slate-400 bg-slate-100 dark:bg-slate-800 px-1.5 py-0.5 rounded"
										title="Pending transaction"
									>
										Pending
									</span>
								{/if}

								{#if isQueued}
									<span
										class="inline-flex items-center text-[10px] text-sky-700 dark:text-sky-300 bg-sky-50 dark:bg-sky-500/15 border border-sky-200 dark:border-sky-500/30 px-1.5 py-0.5 rounded"
									>
										Queued
									</span>
								{/if}

								{#if tx.settledDelta != null && tx.settledDelta !== 0}
									<span
										class="inline-flex items-center text-[10px] text-amber-700 dark:text-amber-300 bg-amber-50 dark:bg-amber-500/15 border border-amber-200 dark:border-amber-500/30 px-1 py-0.5 rounded"
									>
										Δ {tx.settledDelta > 0 ? '+' : '-'}${Math.abs(tx.settledDelta).toFixed(2)}
									</span>
								{/if}
							</div>

							<div class="flex items-center gap-2 mt-0.5 text-[11px] text-slate-500 dark:text-slate-400">
								<span class="truncate">{acct?.name ?? 'Account'}</span>
								{#if !isQueued}
									{#if confirmDeleteId === tx.id}
										<span class="inline-flex items-center gap-1">
											<button
												type="button"
												disabled={isSaving}
												onclick={() => void handleDelete(tx.id)}
												class="text-rose-600 dark:text-rose-400 font-semibold hover:underline"
											>
												Confirm
											</button>
											<button
												type="button"
												onclick={() => (confirmDeleteId = null)}
												class="text-slate-400 hover:text-slate-600 dark:hover:text-slate-200"
											>
												Cancel
											</button>
										</span>
									{:else}
										<button
											type="button"
											onclick={() => (confirmDeleteId = tx.id)}
											class="text-slate-400 hover:text-rose-600 dark:hover:text-rose-400 opacity-0 group-hover:opacity-100 focus:opacity-100 transition-opacity"
											aria-label={`Delete ${displayMerchant}`}
										>
											Delete
										</button>
									{/if}
								{/if}
							</div>
						</div>
					</div>

					<!-- Amount + Quick Categorize Pill -->
					<div class="shrink-0 flex flex-col items-end gap-1">
						<span
							class="text-xs font-mono font-bold {isPending
								? 'italic text-slate-500 dark:text-slate-400'
								: tx.amount > 0
									? 'text-emerald-600 dark:text-emerald-400'
									: 'text-slate-900 dark:text-slate-100'}"
						>
							{formatCurrency(tx.amount)}
						</span>

						{#if isQueued}
							<span class="text-[10px] rounded-full border border-slate-200 dark:border-slate-700 px-2 py-0.5 text-slate-500 dark:text-slate-400">
								{tx.categoryId ? categoryById.get(tx.categoryId)?.name ?? 'Categorized' : 'Queued'}
							</span>
						{:else if tx.isTransfer}
							<span class="text-[10px] rounded-full border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-800 px-2 py-0.5 text-slate-500 dark:text-slate-300">
								Transfer
							</span>
						{:else if isOffBudget}
							<span class="text-[10px] rounded-full border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-800 px-2 py-0.5 text-slate-500 dark:text-slate-300">
								Off-Budget
							</span>
						{:else if hasSplits}
							<span class="text-[10px] rounded-full border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-800 px-2 py-0.5 text-slate-600 dark:text-slate-300">
								Split ({tx.splits?.length})
							</span>
						{:else}
							<select
								aria-label={`Category for ${displayMerchant}`}
								data-testid="tx-category-select"
								value={tx.categoryId ?? ''}
								disabled={isSaving}
								onchange={(e) => void handleCategorySelect(tx.id, e.currentTarget.value)}
								class="max-w-[140px] truncate rounded-full border px-2 py-0.5 text-[11px] font-medium cursor-pointer transition-colors focus:outline-none {tx.categoryId === null
									? 'border-sky-300 dark:border-sky-500/40 bg-sky-50 dark:bg-sky-500/10 text-sky-700 dark:text-sky-300 hover:bg-sky-100 dark:hover:bg-sky-500/20'
									: 'border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 text-slate-700 dark:text-slate-200 hover:border-slate-300 dark:hover:border-slate-600'}"
							>
								<option value="">{tx.categoryId === null ? '+ Categorize' : 'Uncategorized'}</option>
								{#each groupedCategories as [grp, cats]}
									<optgroup label={grp}>
										{#each cats as cat}
											<option value={cat.id}>{cat.name}</option>
										{/each}
									</optgroup>
								{/each}
							</select>
						{/if}
					</div>
				</div>
			{/each}
		{/if}
	</div>
</div>
