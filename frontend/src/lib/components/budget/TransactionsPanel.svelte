<script lang="ts">
	import type { OfflineTransaction } from '$lib/offline/db';
	import type { CategoryBudget, PlannedIncome } from '$lib/types/budget';
	import type {
		AccountItem,
		TransactionItemModel,
		UpdateTransactionPayload
	} from '$lib/types/transaction';
	import {
		getReviewBadgeKind,
		isPendingTransaction,
		isToReviewTransaction,
		mapOfflineQueuedToTransactions
	} from '$lib/utils/transactionReview';

	type TxSubTab = 'to_review' | 'all';

	interface Props {
		currentMonth: string;
		transactions: TransactionItemModel[];
		queuedTransactions: OfflineTransaction[];
		accounts: AccountItem[];
		categories: CategoryBudget[];
		incomes?: PlannedIncome[];
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
		onAssignTransactionToIncome?: (txId: string, incomeId: string | null) => Promise<void>;
		onUpdateTransaction?: (txId: string, payload: UpdateTransactionPayload) => Promise<void>;
		onDeleteTransaction: (txId: string) => Promise<void>;
		onSyncNow: () => Promise<void>;
		onTransactionDragStart?: (txId: string) => void;
		onTransactionDragEnd?: () => void;
	}

	let {
		currentMonth,
		transactions,
		queuedTransactions,
		accounts,
		categories,
		incomes = [],
		isOffline,
		syncing,
		activeCategoryFilterId,
		onClearCategoryFilter,
		onCreateTransaction,
		onCategorizeTransaction,
		onAssignTransactionToIncome,
		onUpdateTransaction,
		onDeleteTransaction,
		onSyncNow,
		onTransactionDragStart,
		onTransactionDragEnd
	}: Props = $props();

	let activeSubTab = $state<TxSubTab>('to_review');
	let searchQuery = $state('');
	let selectedAccountId = $state('');
	let showQuickAdd = $state(false);
	let savingIds = $state<Record<string, boolean>>({});
	let actionError = $state<string | null>(null);

	// Quick Category Picker, Overflow Menu & Inline Edit State
	let openPickerTxId = $state<string | null>(null);
	let pickerFilter = $state('');
	let openMenuTxId = $state<string | null>(null);
	let confirmDeleteId = $state<string | null>(null);
	let editingTxId = $state<string | null>(null);
	let editMerchant = $state('');
	let editNotes = $state('');
	let editCategoryId = $state('');

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
		// When user filters by a specific category from the left budget list, ensure All can show it
		if (activeCategoryFilterId && activeSubTab === 'to_review') {
			activeSubTab = 'all';
		}
	});

	$effect(() => {
		if (!openMenuTxId && !openPickerTxId && !editingTxId && !confirmDeleteId) return;

		function handleKeyDown(e: KeyboardEvent) {
			if (e.key === 'Escape') {
				openMenuTxId = null;
				confirmDeleteId = null;
				openPickerTxId = null;
				editingTxId = null;
			}
		}

		function handleDocumentClick(e: MouseEvent) {
			if (!openMenuTxId && !confirmDeleteId) return;
			const target = e.target as Element | null;
			if (!target?.closest('[data-tx-menu-container]')) {
				openMenuTxId = null;
				confirmDeleteId = null;
			}
		}

		window.addEventListener('keydown', handleKeyDown);
		document.addEventListener('click', handleDocumentClick);
		return () => {
			window.removeEventListener('keydown', handleKeyDown);
			document.removeEventListener('click', handleDocumentClick);
		};
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
	let incomeById = $derived(new Map(incomes.map((i) => [i.id, i])));

	let groupedCategories = $derived.by(() => {
		const groups = new Map<string, CategoryBudget[]>();
		for (const cat of categories) {
			const list = groups.get(cat.groupName) ?? [];
			list.push(cat);
			groups.set(cat.groupName, list);
		}
		return Array.from(groups.entries());
	});

	let filteredPickerGroups = $derived.by(() => {
		const q = pickerFilter.trim().toLowerCase();
		if (!q) return groupedCategories;
		return groupedCategories
			.map(
				([groupName, cats]) =>
					[
						groupName,
						cats.filter(
							(c) =>
								c.name.toLowerCase().includes(q) || groupName.toLowerCase().includes(q)
						)
					] as [string, CategoryBudget[]]
			)
			.filter(([, cats]) => cats.length > 0);
	});

	let filteredPickerIncomes = $derived.by(() => {
		const q = pickerFilter.trim().toLowerCase();
		if (!q) return incomes;
		return incomes.filter(
			(inc) => inc.name.toLowerCase().includes(q) || 'income'.includes(q)
		);
	});

	let combinedItems = $derived.by(() => {
		const offlineMapped = mapOfflineQueuedToTransactions(queuedTransactions, currentMonth);
		return [...offlineMapped, ...transactions];
	});

	let toReviewItems = $derived(
		combinedItems.filter((tx) => isToReviewTransaction(tx, accountById))
	);

	let filteredItems = $derived.by(() => {
		let base = activeSubTab === 'to_review' ? toReviewItems : combinedItems;

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

	const SUB_TABS: Array<{ id: TxSubTab; label: (reviewCount: number, allCount: number) => string }> = [
		{ id: 'to_review', label: (r) => `To Review (${r})` },
		{ id: 'all', label: (_r, a) => `All (${a})` }
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

	function toggleQuickPicker(txId: string) {
		openMenuTxId = null;
		confirmDeleteId = null;
		editingTxId = null;
		if (openPickerTxId === txId) {
			openPickerTxId = null;
		} else {
			openPickerTxId = txId;
			pickerFilter = '';
		}
	}

	function openRowMenu(txId: string, e: MouseEvent) {
		e.stopPropagation();
		if (openMenuTxId === txId) {
			openMenuTxId = null;
			confirmDeleteId = null;
		} else {
			openMenuTxId = txId;
			confirmDeleteId = null;
		}
	}

	function startEditDetails(tx: TransactionItemModel) {
		openMenuTxId = null;
		confirmDeleteId = null;
		openPickerTxId = null;
		editingTxId = tx.id;
		editMerchant = tx.merchantClean || tx.merchantRaw;
		editNotes = tx.userNotes || '';
		editCategoryId = tx.categoryId ?? '';
	}

	async function handleCategorySelect(txId: string, rawVal: string) {
		const nextCatId = rawVal === '' ? null : rawVal;
		savingIds = { ...savingIds, [txId]: true };
		actionError = null;
		try {
			await onCategorizeTransaction(txId, nextCatId);
			openPickerTxId = null;
			pickerFilter = '';
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to categorize transaction.';
		} finally {
			const next = { ...savingIds };
			delete next[txId];
			savingIds = next;
		}
	}

	async function handleIncomeSelect(txId: string, incomeId: string | null) {
		if (!onAssignTransactionToIncome) return;
		savingIds = { ...savingIds, [txId]: true };
		actionError = null;
		try {
			await onAssignTransactionToIncome(txId, incomeId);
			openPickerTxId = null;
			pickerFilter = '';
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to assign transaction to income.';
		} finally {
			const next = { ...savingIds };
			delete next[txId];
			savingIds = next;
		}
	}

	async function handleEditSubmit(e: SubmitEvent, tx: TransactionItemModel, canCategorize: boolean) {
		e.preventDefault();
		const trimmedMerchant = editMerchant.trim();
		if (!trimmedMerchant) return;

		savingIds = { ...savingIds, [tx.id]: true };
		actionError = null;
		try {
			const nextCatId = editCategoryId === '' ? null : editCategoryId;
			if (onUpdateTransaction) {
				const payload: UpdateTransactionPayload = {
					merchantClean: trimmedMerchant,
					userNotes: editNotes.trim()
				};
				if (canCategorize) {
					if (nextCatId) {
						payload.categoryId = nextCatId;
					} else {
						payload.clearCategory = true;
					}
				}
				await onUpdateTransaction(tx.id, payload);
			} else if (canCategorize && nextCatId !== tx.categoryId) {
				await onCategorizeTransaction(tx.id, nextCatId);
			}
			editingTxId = null;
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to update transaction.';
		} finally {
			const next = { ...savingIds };
			delete next[tx.id];
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
			openMenuTxId = null;
			if (openPickerTxId === txId) openPickerTxId = null;
			if (editingTxId === txId) editingTxId = null;
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

<div class="flex flex-col h-full min-h-0 bg-surface text-ink-primary" data-testid="transactions-panel">
	<!-- Sub-Tabs: To Review + All -->
	<div
		role="tablist"
		aria-label="Transaction status filter"
		class="grid grid-cols-2 border-b border-border px-2 shrink-0 bg-surface"
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
					? 'text-accent-pop after:content-[""] after:absolute after:inset-x-3 after:bottom-0 after:h-0.5 after:bg-accent-pop'
					: 'text-ink-secondary hover:text-ink-primary'}"
			>
				{tab.label(toReviewItems.length, combinedItems.length)}
			</button>
		{/each}
	</div>

	<!-- Search + Filter + Quick-Add Trigger -->
	<div class="p-3 space-y-2 border-b border-border shrink-0 bg-surface">
		<div class="flex items-center gap-2">
			<div class="relative flex-1">
				<svg
					class="w-3.5 h-3.5 text-ink-muted absolute left-2.5 top-1/2 -translate-y-1/2 pointer-events-none"
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
					class="w-full rounded-lg border border-border bg-subtle pl-8 pr-3 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
				/>
			</div>

			<button
				type="button"
				onclick={() => (showQuickAdd = !showQuickAdd)}
				aria-expanded={showQuickAdd}
				class="shrink-0 rounded-lg px-2.5 py-1.5 text-xs font-semibold transition-opacity {showQuickAdd
					? 'bg-subtle border border-border text-ink-primary hover:bg-surface-hover'
					: 'bg-accent-primary text-accent-primary-text hover:opacity-90'}"
			>
				{showQuickAdd ? 'Close' : '+ Add'}
			</button>
		</div>

		<div class="flex flex-wrap items-center gap-2">
			<select
				aria-label="Filter by account"
				bind:value={selectedAccountId}
				class="rounded-md border border-border bg-surface px-2 py-1 text-[11px] text-ink-secondary focus:border-accent-pop focus:outline-none"
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
					class="inline-flex items-center gap-1 rounded-full bg-accent-tint border border-accent-pop/30 px-2 py-0.5 text-[11px] font-medium text-accent-pop"
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
					class="ml-auto rounded-md bg-accent-pop text-accent-primary-text disabled:opacity-50 px-2 py-0.5 text-[11px] font-medium hover:opacity-90 transition-opacity"
				>
					{syncing ? 'Syncing...' : `Sync (${queuedTransactions.length})`}
				</button>
			{/if}
		</div>

		{#if showQuickAdd}
			<form
				onsubmit={handleQuickAddSubmit}
				data-testid="quick-add-tx-form"
				class="rounded-xl border border-border bg-subtle p-3 space-y-2.5"
			>
				<div class="grid grid-cols-2 gap-2">
					<div>
						<label class="block text-[11px] text-ink-secondary mb-0.5" for="qa-account">
							Account
						</label>
						<select
							id="qa-account"
							bind:value={formAccountId}
							required
							class="w-full rounded-md border border-border bg-surface px-2 py-1 text-xs text-ink-primary focus:border-accent-pop focus:outline-none"
						>
							{#each accounts as acct}
								<option value={acct.id}>{acct.name}</option>
							{/each}
						</select>
					</div>

					<div>
						<span class="block text-[11px] text-ink-secondary mb-0.5">Type</span>
						<div class="grid grid-cols-2 rounded-md border border-border bg-surface p-0.5 text-[11px]">
							<button
								type="button"
								onclick={() => (formDirection = 'expense')}
								class="rounded py-0.5 font-medium transition-colors {formDirection === 'expense'
									? 'bg-status-danger-bg text-status-danger'
									: 'text-ink-secondary hover:text-ink-primary'}"
							>
								Expense
							</button>
							<button
								type="button"
								onclick={() => (formDirection = 'inflow')}
								class="rounded py-0.5 font-medium transition-colors {formDirection === 'inflow'
									? 'bg-status-good-bg text-status-good'
									: 'text-ink-secondary hover:text-ink-primary'}"
							>
								Inflow
							</button>
						</div>
					</div>
				</div>

				<div class="grid grid-cols-3 gap-2">
					<div class="col-span-2">
						<label class="block text-[11px] text-ink-secondary mb-0.5" for="qa-merchant">
							Merchant / Payee
						</label>
						<input
							id="qa-merchant"
							type="text"
							bind:value={formMerchant}
							placeholder={formDirection === 'inflow' ? 'Paycheck / Deposit' : 'Trader Joe\'s'}
							required
							class="w-full rounded-md border border-border bg-surface px-2 py-1 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
						/>
					</div>

					<div>
						<label class="block text-[11px] text-ink-secondary mb-0.5" for="qa-amount">
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
							class="w-full rounded-md border border-border bg-surface px-2 py-1 text-xs font-mono text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
						/>
					</div>
				</div>

				<div class="grid grid-cols-2 gap-2">
					<div>
						<label class="block text-[11px] text-ink-secondary mb-0.5" for="qa-date">
							Date
						</label>
						<input
							id="qa-date"
							type="date"
							bind:value={formDate}
							oninput={() => (formDateDirty = true)}
							required
							class="w-full rounded-md border border-border bg-surface px-2 py-1 text-xs text-ink-primary focus:border-accent-pop focus:outline-none"
						/>
					</div>

					<div>
						<label class="block text-[11px] text-ink-secondary mb-0.5" for="qa-category">
							Category
						</label>
						<select
							id="qa-category"
							bind:value={formCategoryId}
							disabled={isOffBudgetFormAccount}
							class="w-full rounded-md border border-border bg-surface px-2 py-1 text-xs text-ink-primary disabled:opacity-50 focus:border-accent-pop focus:outline-none"
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
						class="w-full rounded-lg bg-accent-primary text-accent-primary-text hover:opacity-90 disabled:opacity-50 py-1.5 text-xs font-semibold transition-opacity"
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
			<p class="text-[11px] text-status-danger bg-status-danger-bg border border-status-danger/30 rounded-md px-2.5 py-1.5">
				{actionError}
			</p>
		{/if}
	</div>

	<!-- Transaction Feed -->
	<div class="flex-1 overflow-y-auto overscroll-contain-scroll divide-y divide-border">
		{#if filteredItems.length === 0}
			<div class="px-4 py-10 text-center space-y-1">
				<p class="text-xs font-medium text-ink-secondary">
					{#if activeSubTab === 'to_review'}
						All caught up — no transactions to review!
					{:else}
						No matching transactions for {currentMonth}.
					{/if}
				</p>
				<p class="text-[11px] text-ink-muted">
					Click "+ Add" above to record one manually.
				</p>
			</div>
		{:else}
			{#each filteredItems as tx (tx.id)}
				{@const badge = parseDateBadge(tx.transactionDate)}
				{@const acct = accountById.get(tx.accountId)}
				{@const isPending = isPendingTransaction(tx)}
				{@const isQueued = tx.origin === 'MANUAL_OFFLINE' && tx.syncStatus === 'QUEUED'}
				{@const hasSplits = Boolean(tx.splits && tx.splits.length > 0)}
				{@const isOffBudget = acct ? !acct.isOnBudget : false}
				{@const canCategorize = !isQueued && !tx.isTransfer && !isOffBudget && !hasSplits}
				{@const badgeKind = getReviewBadgeKind(tx, accountById)}
				{@const displayMerchant = tx.merchantClean || tx.merchantRaw}
				{@const isSaving = Boolean(savingIds[tx.id])}
				{@const assignedCategory = tx.categoryId ? categoryById.get(tx.categoryId) : null}
				{@const assignedIncome = tx.plannedIncomeId ? incomeById.get(tx.plannedIncomeId) : null}
				{@const isUnassigned = tx.categoryId === null && !tx.plannedIncomeId}
				{@const isPickerOpen = openPickerTxId === tx.id}
				{@const isMenuOpen = openMenuTxId === tx.id}
				{@const isEditing = editingTxId === tx.id}

				<!-- svelte-ignore a11y_no_static_element_interactions -->
				<div
					data-testid="tx-panel-row"
					draggable={canCategorize}
					ondragstart={(e) => {
						if (!canCategorize) return;
						e.dataTransfer?.setData('application/x-budget-tx-id', tx.id);
						e.dataTransfer?.setData('text/plain', tx.id);
						if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move';
						onTransactionDragStart?.(tx.id);
					}}
					ondragend={() => {
						onTransactionDragEnd?.();
					}}
					class="transition-colors {canCategorize ? 'cursor-grab active:cursor-grabbing' : ''}"
				>
					{#if canCategorize}
						<div
							role="button"
							tabindex="0"
							aria-expanded={isPickerOpen}
							onclick={() => toggleQuickPicker(tx.id)}
							onkeydown={(e) => {
								if (e.target !== e.currentTarget) return;
								if (e.key === 'Enter' || e.key === ' ') {
									e.preventDefault();
									toggleQuickPicker(tx.id);
								}
							}}
							class="group px-3.5 py-3 flex items-start justify-between gap-2.5 cursor-pointer hover:bg-surface-hover/40 transition-colors"
						>
							<!-- Circular Date Badge + Merchant + Account -->
							<div class="flex items-start gap-2.5 min-w-0 flex-1">
								<div
									class="w-9 h-9 rounded-full border border-border bg-subtle flex flex-col items-center justify-center shrink-0 leading-none"
									aria-label={tx.transactionDate}
								>
									<span class="text-[9px] uppercase font-semibold text-accent-pop">
										{badge.month}
									</span>
									<span class="text-xs font-bold text-ink-primary mt-0.5">
										{badge.day}
									</span>
								</div>

								<div class="min-w-0 flex-1">
									<div class="flex items-center gap-1.5 flex-wrap">
										<span
											class="text-xs font-semibold truncate {isPending
												? 'italic text-ink-secondary'
												: 'text-ink-primary'}"
										>
											{displayMerchant}
										</span>

										{#if badgeKind === 'pending'}
											<span
												data-testid="tx-badge-pending"
												class="inline-flex items-center text-[10px] font-medium bg-status-warn-bg text-status-warn px-1.5 py-0.5 rounded"
												title="Pending transaction"
											>
												Pending
											</span>
										{:else if badgeKind === 'new'}
											<span
												data-testid="tx-badge-new"
												class="inline-flex items-center text-[10px] font-medium bg-accent-tint text-accent-pop px-1.5 py-0.5 rounded"
												title="Uncategorized transaction"
											>
												New
											</span>
										{/if}

										{#if tx.settledDelta != null && tx.settledDelta !== 0}
											<span
												class="inline-flex items-center text-[10px] font-medium bg-status-warn-bg text-status-warn border border-status-warn/30 px-1 py-0.5 rounded"
											>
												Δ {tx.settledDelta > 0 ? '+' : '-'}${Math.abs(tx.settledDelta).toFixed(2)}
											</span>
										{/if}
									</div>

									<div class="flex items-center gap-2 mt-0.5 text-[11px] text-ink-secondary">
										<span class="truncate">{acct?.name ?? 'Account'}</span>
										{#if tx.userNotes}
											<span class="truncate text-ink-muted">· {tx.userNotes}</span>
										{/if}
									</div>
								</div>
							</div>

							<!-- Amount + Category Pill + Overflow Menu -->
							<div class="shrink-0 flex items-start gap-1.5">
								<div class="flex flex-col items-end gap-1">
									<span
										class="text-xs font-mono font-bold {isPending
											? 'italic text-ink-secondary'
											: tx.amount > 0
												? 'text-status-good'
												: 'text-ink-primary'}"
									>
										{formatCurrency(tx.amount)}
									</span>

									<button
										type="button"
										data-testid="tx-category-pill"
										aria-label={`Category for ${displayMerchant}`}
										disabled={isSaving}
										onclick={(e) => {
											e.stopPropagation();
											toggleQuickPicker(tx.id);
										}}
										class="max-w-[110px] sm:max-w-[140px] truncate rounded-full border px-2 py-0.5 text-[11px] font-medium cursor-pointer transition-colors focus:outline-none {isUnassigned
											? 'border-accent-pop/40 bg-accent-tint text-accent-pop hover:opacity-90'
											: 'border-border bg-subtle text-ink-secondary hover:text-ink-primary hover:bg-surface-hover'}"
									>
										{assignedCategory?.name ??
											tx.categoryName ??
											(tx.plannedIncomeId
												? assignedIncome
													? `Income: ${assignedIncome.name}`
													: 'Income'
												: '+ Categorize')}
									</button>
								</div>

								<div class="relative" data-tx-menu-container>
									<button
										type="button"
										data-testid="tx-row-menu-btn"
										aria-label={`More actions for ${displayMerchant}`}
										aria-haspopup="menu"
										aria-expanded={isMenuOpen}
										onclick={(e) => openRowMenu(tx.id, e)}
										class="p-1 rounded-md text-ink-muted hover:text-ink-primary hover:bg-surface-hover transition-colors"
									>
										<svg class="w-4 h-4" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
											<circle cx="12" cy="5" r="1.75" />
											<circle cx="12" cy="12" r="1.75" />
											<circle cx="12" cy="19" r="1.75" />
										</svg>
									</button>

									{#if isMenuOpen}
										<div
											role="menu"
											tabindex="-1"
											data-testid="tx-row-menu"
											onclick={(e) => e.stopPropagation()}
											onkeydown={(e) => e.stopPropagation()}
											class="absolute right-0 top-full mt-1 z-30 w-44 rounded-xl border border-border bg-surface shadow-lg py-1 text-xs"
										>
											<button
												type="button"
												role="menuitem"
												data-testid="tx-menu-categorize"
												onclick={() => {
													openMenuTxId = null;
													openPickerTxId = tx.id;
													pickerFilter = '';
													editingTxId = null;
												}}
												class="w-full text-left px-3 py-1.5 text-ink-primary hover:bg-surface-hover transition-colors"
											>
												Categorize
											</button>
											<button
												type="button"
												role="menuitem"
												data-testid="tx-menu-edit"
												onclick={() => startEditDetails(tx)}
												class="w-full text-left px-3 py-1.5 text-ink-primary hover:bg-surface-hover transition-colors"
											>
												Edit details
											</button>
											<div class="my-1 border-t border-border"></div>
											{#if confirmDeleteId === tx.id}
												<div class="px-3 py-1.5 space-y-1.5">
													<p class="text-[11px] font-medium text-status-danger">
														Delete this transaction?
													</p>
													<div class="flex items-center gap-1.5">
														<button
															type="button"
															data-testid="tx-menu-confirm-delete"
															disabled={isSaving}
															onclick={() => void handleDelete(tx.id)}
															class="flex-1 rounded-md bg-status-danger-bg text-status-danger px-2 py-1 text-[11px] font-semibold hover:opacity-90"
														>
															Confirm Delete
														</button>
														<button
															type="button"
															data-testid="tx-menu-cancel-delete"
															onclick={() => (confirmDeleteId = null)}
															class="rounded-md border border-border bg-subtle px-2 py-1 text-[11px] text-ink-secondary hover:text-ink-primary"
														>
															Cancel
														</button>
													</div>
												</div>
											{:else}
												<button
													type="button"
													role="menuitem"
													data-testid="tx-menu-delete"
													onclick={() => (confirmDeleteId = tx.id)}
													class="w-full text-left px-3 py-1.5 text-status-danger hover:bg-status-danger-bg transition-colors"
												>
													Delete transaction
												</button>
											{/if}
										</div>
									{/if}
								</div>
							</div>
						</div>
					{:else}
						<div class="group px-3.5 py-3 flex items-start justify-between gap-2.5 hover:bg-surface-hover/40 transition-colors">
							<!-- Circular Date Badge + Merchant + Account -->
							<div class="flex items-start gap-2.5 min-w-0 flex-1">
								<div
									class="w-9 h-9 rounded-full border border-border bg-subtle flex flex-col items-center justify-center shrink-0 leading-none"
									aria-label={tx.transactionDate}
								>
									<span class="text-[9px] uppercase font-semibold text-accent-pop">
										{badge.month}
									</span>
									<span class="text-xs font-bold text-ink-primary mt-0.5">
										{badge.day}
									</span>
								</div>

								<div class="min-w-0 flex-1">
									<div class="flex items-center gap-1.5 flex-wrap">
										<span
											class="text-xs font-semibold truncate {isPending
												? 'italic text-ink-secondary'
												: 'text-ink-primary'}"
										>
											{displayMerchant}
										</span>

										{#if badgeKind === 'pending'}
											<span
												data-testid="tx-badge-pending"
												class="inline-flex items-center text-[10px] font-medium bg-status-warn-bg text-status-warn px-1.5 py-0.5 rounded"
												title="Pending transaction"
											>
												Pending
											</span>
										{:else if badgeKind === 'new'}
											<span
												data-testid="tx-badge-new"
												class="inline-flex items-center text-[10px] font-medium bg-accent-tint text-accent-pop px-1.5 py-0.5 rounded"
												title="Uncategorized transaction"
											>
												New
											</span>
										{/if}

										{#if isQueued}
											<span
												class="inline-flex items-center text-[10px] font-medium bg-accent-tint text-accent-pop border border-accent-pop/30 px-1.5 py-0.5 rounded"
											>
												Queued
											</span>
										{/if}

										{#if tx.settledDelta != null && tx.settledDelta !== 0}
											<span
												class="inline-flex items-center text-[10px] font-medium bg-status-warn-bg text-status-warn border border-status-warn/30 px-1 py-0.5 rounded"
											>
												Δ {tx.settledDelta > 0 ? '+' : '-'}${Math.abs(tx.settledDelta).toFixed(2)}
											</span>
										{/if}
									</div>

									<div class="flex items-center gap-2 mt-0.5 text-[11px] text-ink-secondary">
										<span class="truncate">{acct?.name ?? 'Account'}</span>
										{#if tx.userNotes}
											<span class="truncate text-ink-muted">· {tx.userNotes}</span>
										{/if}
									</div>
								</div>
							</div>

							<!-- Amount + Static Pill + Optional Overflow Menu -->
							<div class="shrink-0 flex items-start gap-1.5">
								<div class="flex flex-col items-end gap-1">
									<span
										class="text-xs font-mono font-bold {isPending
											? 'italic text-ink-secondary'
											: tx.amount > 0
												? 'text-status-good'
												: 'text-ink-primary'}"
									>
										{formatCurrency(tx.amount)}
									</span>

									{#if isQueued}
										<span class="text-[10px] rounded-full border border-border bg-subtle px-2 py-0.5 text-ink-secondary">
											{tx.categoryId ? categoryById.get(tx.categoryId)?.name ?? 'Categorized' : 'Queued'}
										</span>
									{:else if tx.isTransfer}
										<span class="text-[10px] rounded-full border border-border bg-subtle px-2 py-0.5 text-ink-secondary">
											Transfer
										</span>
									{:else if isOffBudget}
										<span class="text-[10px] rounded-full border border-border bg-subtle px-2 py-0.5 text-ink-secondary">
											Off-Budget
										</span>
									{:else if hasSplits}
										<span class="text-[10px] rounded-full border border-border bg-subtle px-2 py-0.5 text-ink-secondary">
											Split ({tx.splits?.length})
										</span>
									{/if}
								</div>

								{#if !isQueued}
									<div class="relative" data-tx-menu-container>
										<button
											type="button"
											data-testid="tx-row-menu-btn"
											aria-label={`More actions for ${displayMerchant}`}
											aria-haspopup="menu"
											aria-expanded={isMenuOpen}
											onclick={(e) => openRowMenu(tx.id, e)}
											class="p-1 rounded-md text-ink-muted hover:text-ink-primary hover:bg-surface-hover transition-colors"
										>
											<svg class="w-4 h-4" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
												<circle cx="12" cy="5" r="1.75" />
												<circle cx="12" cy="12" r="1.75" />
												<circle cx="12" cy="19" r="1.75" />
											</svg>
										</button>

										{#if isMenuOpen}
											<div
												role="menu"
												tabindex="-1"
												data-testid="tx-row-menu"
												onclick={(e) => e.stopPropagation()}
												onkeydown={(e) => e.stopPropagation()}
												class="absolute right-0 top-full mt-1 z-30 w-44 rounded-xl border border-border bg-surface shadow-lg py-1 text-xs"
											>
												<button
													type="button"
													role="menuitem"
													data-testid="tx-menu-edit"
													onclick={() => startEditDetails(tx)}
													class="w-full text-left px-3 py-1.5 text-ink-primary hover:bg-surface-hover transition-colors"
												>
													Edit details
												</button>
												<div class="my-1 border-t border-border"></div>
												{#if confirmDeleteId === tx.id}
													<div class="px-3 py-1.5 space-y-1.5">
														<p class="text-[11px] font-medium text-status-danger">
															Delete this transaction?
														</p>
														<div class="flex items-center gap-1.5">
															<button
																type="button"
																data-testid="tx-menu-confirm-delete"
																disabled={isSaving}
																onclick={() => void handleDelete(tx.id)}
																class="flex-1 rounded-md bg-status-danger-bg text-status-danger px-2 py-1 text-[11px] font-semibold hover:opacity-90"
															>
																Confirm Delete
															</button>
															<button
																type="button"
																data-testid="tx-menu-cancel-delete"
																onclick={() => (confirmDeleteId = null)}
																class="rounded-md border border-border bg-subtle px-2 py-1 text-[11px] text-ink-secondary hover:text-ink-primary"
															>
																Cancel
															</button>
														</div>
													</div>
												{:else}
													<button
														type="button"
														role="menuitem"
														data-testid="tx-menu-delete"
														onclick={() => (confirmDeleteId = tx.id)}
														class="w-full text-left px-3 py-1.5 text-status-danger hover:bg-status-danger-bg transition-colors"
													>
														Delete transaction
													</button>
												{/if}
											</div>
										{/if}
									</div>
								{/if}
							</div>
						</div>
					{/if}

					<!-- Inline Quick Category Picker -->
					{#if isPickerOpen && canCategorize}
						<div
							role="region"
							aria-label={`Quick category picker for ${displayMerchant}`}
							data-testid="quick-category-picker"
							class="mx-3.5 mb-3 rounded-xl border border-border bg-subtle p-2.5 space-y-2"
						>
							<div class="flex items-center justify-between gap-2">
								<span class="text-[11px] font-semibold text-ink-primary">
									Sort into category
								</span>
								<button
									type="button"
									aria-label="Close category picker"
									onclick={() => (openPickerTxId = null)}
									class="text-[11px] text-ink-muted hover:text-ink-primary px-1"
								>
									✕
								</button>
							</div>

							<input
								type="search"
								aria-label="Filter categories"
								placeholder="Filter categories..."
								bind:value={pickerFilter}
								class="w-full rounded-md border border-border bg-surface px-2 py-1 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
							/>

							<div class="max-h-44 overflow-y-auto space-y-2">
								{#if filteredPickerGroups.length === 0 && (!onAssignTransactionToIncome || filteredPickerIncomes.length === 0)}
									<p class="text-[11px] text-ink-muted py-1 text-center">
										No matching categories
									</p>
								{:else}
									{#if onAssignTransactionToIncome && filteredPickerIncomes.length > 0}
										<div class="space-y-1">
											<p class="text-[10px] font-semibold uppercase tracking-wider text-ink-muted">
												Income
											</p>
											<div class="flex flex-wrap gap-1.5">
												{#each filteredPickerIncomes as inc (inc.id)}
													{@const isAssignedIncome = tx.plannedIncomeId === inc.id}
													<button
														type="button"
														data-testid="quick-income-option"
														disabled={isSaving}
														onclick={() => void handleIncomeSelect(tx.id, inc.id)}
														class="rounded-full border px-2.5 py-0.5 text-[11px] font-medium transition-colors {isAssignedIncome
															? 'border-status-good bg-status-good-bg text-status-good font-semibold'
															: 'border-border bg-surface text-ink-primary hover:bg-surface-hover'}"
													>
														{inc.name}
													</button>
												{/each}
											</div>
										</div>
									{/if}
									{#each filteredPickerGroups as [grp, cats]}
										<div class="space-y-1">
											<p class="text-[10px] font-semibold uppercase tracking-wider text-ink-muted">
												{grp}
											</p>
											<div class="flex flex-wrap gap-1.5">
												{#each cats as cat}
													{@const isAssigned = tx.categoryId === cat.id}
													<button
														type="button"
														data-testid="quick-category-option"
														disabled={isSaving}
														onclick={() => void handleCategorySelect(tx.id, cat.id)}
														class="rounded-full border px-2.5 py-0.5 text-[11px] font-medium transition-colors {isAssigned
															? 'border-accent-pop bg-accent-tint text-accent-pop font-semibold'
															: 'border-border bg-surface text-ink-primary hover:bg-surface-hover'}"
													>
														{cat.name}
													</button>
												{/each}
											</div>
										</div>
									{/each}
								{/if}

								{#if !isUnassigned}
									<div class="pt-1 border-t border-border">
										<button
											type="button"
											data-testid="quick-category-clear"
											disabled={isSaving}
											onclick={() =>
												void (tx.plannedIncomeId && onAssignTransactionToIncome
													? handleIncomeSelect(tx.id, null)
													: handleCategorySelect(tx.id, ''))}
											class="rounded-full border border-border bg-surface px-2.5 py-0.5 text-[11px] font-medium text-ink-secondary hover:text-ink-primary hover:bg-surface-hover"
										>
											Uncategorized
										</button>
									</div>
								{/if}
							</div>
						</div>
					{/if}

					<!-- Inline Edit Details Form -->
					{#if isEditing && !isQueued}
						<form
							data-testid="tx-edit-form"
							onsubmit={(e) => void handleEditSubmit(e, tx, canCategorize)}
							class="mx-3.5 mb-3 rounded-xl border border-border bg-subtle p-2.5 space-y-2"
						>
							<div class="flex items-center justify-between">
								<span class="text-[11px] font-semibold text-ink-primary">
									Edit transaction details
								</span>
								<button
									type="button"
									aria-label="Close edit form"
									onclick={() => (editingTxId = null)}
									class="text-[11px] text-ink-muted hover:text-ink-primary px-1"
								>
									✕
								</button>
							</div>

							<div>
								<label class="block text-[10px] text-ink-secondary mb-0.5" for={`edit-merchant-${tx.id}`}>
									Merchant
								</label>
								<input
									id={`edit-merchant-${tx.id}`}
									aria-label="Edit merchant"
									type="text"
									required
									bind:value={editMerchant}
									class="w-full rounded-md border border-border bg-surface px-2 py-1 text-xs text-ink-primary focus:border-accent-pop focus:outline-none"
								/>
							</div>

							<div>
								<label class="block text-[10px] text-ink-secondary mb-0.5" for={`edit-notes-${tx.id}`}>
									Notes
								</label>
								<input
									id={`edit-notes-${tx.id}`}
									aria-label="Edit notes"
									type="text"
									placeholder="Optional note..."
									bind:value={editNotes}
									class="w-full rounded-md border border-border bg-surface px-2 py-1 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
								/>
							</div>

							{#if canCategorize}
								<div>
									<label class="block text-[10px] text-ink-secondary mb-0.5" for={`edit-cat-${tx.id}`}>
										Category
									</label>
									<select
										id={`edit-cat-${tx.id}`}
										aria-label="Edit category"
										bind:value={editCategoryId}
										class="w-full rounded-md border border-border bg-surface px-2 py-1 text-xs text-ink-primary focus:border-accent-pop focus:outline-none"
									>
										<option value="">Uncategorized</option>
										{#each groupedCategories as [grp, cats]}
											<optgroup label={grp}>
												{#each cats as cat}
													<option value={cat.id}>{cat.name}</option>
												{/each}
											</optgroup>
										{/each}
									</select>
								</div>
							{/if}

							<div class="flex justify-end gap-2 pt-1">
								<button
									type="button"
									onclick={() => (editingTxId = null)}
									class="rounded-md border border-border bg-surface px-2.5 py-1 text-[11px] font-medium text-ink-secondary hover:text-ink-primary"
								>
									Cancel
								</button>
								<button
									type="submit"
									disabled={isSaving || !editMerchant.trim()}
									class="rounded-md bg-accent-primary text-accent-primary-text px-2.5 py-1 text-[11px] font-semibold hover:opacity-90 disabled:opacity-50"
								>
									{isSaving ? 'Saving...' : 'Save changes'}
								</button>
							</div>
						</form>
					{/if}
				</div>
			{/each}
		{/if}
	</div>
</div>
