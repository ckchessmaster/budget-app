<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { fetchMonthBudget, updateMonthlyPlanned } from '$lib/api/budget';
	import {
		createTransaction,
		deleteTransaction,
		fetchAccounts,
		fetchSolvencyReport,
		fetchTransactions,
		updateTransaction
	} from '$lib/api/transactions';
	import BudgetHeader from '$lib/components/budget/BudgetHeader.svelte';
	import CategoryFocusPills, {
		FOCUS_ALL,
		FOCUS_INCOME
	} from '$lib/components/budget/CategoryFocusPills.svelte';
	import CategoryGroupCard from '$lib/components/budget/CategoryGroupCard.svelte';
	import CategoryModal from '$lib/components/budget/CategoryModal.svelte';
	import CatchUpModal, { type CatchUpMode } from '$lib/components/budget/CatchUpModal.svelte';
	import CoverDrawer from '$lib/components/budget/CoverDrawer.svelte';
	import IncomeCard from '$lib/components/budget/IncomeCard.svelte';
	import TransactionsDrawer from '$lib/components/budget/TransactionsDrawer.svelte';
	import TransactionsPanel from '$lib/components/budget/TransactionsPanel.svelte';
	import { BudgetLocalDB, type OfflineTransaction } from '$lib/offline/db';
	import {
		flushOfflineQueue,
		registerOnlineSyncListener,
		requestBackgroundSync
	} from '$lib/offline/sync';
	import { auth } from '$lib/stores/auth.svelte';
	import { demo } from '$lib/stores/demo.svelte';
	import type {
		CategoryBudget,
		InlineActivityItem,
		MonthBudgetDetails
	} from '$lib/types/budget';
	import type {
		AccountItem,
		SolvencyReport,
		TransactionItemModel,
		UpdateTransactionPayload
	} from '$lib/types/transaction';
	import { getGroupColor } from '$lib/utils/budgetColors';
	import {
		isCatchUpTransaction,
		isCategorizableTransaction,
		isToReviewTransaction,
		mapOfflineQueuedToTransactions
	} from '$lib/utils/transactionReview';

	function defaultMonth(): string {
		const now = new Date();
		return `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}`;
	}

	let currentMonth = $derived.by(() => {
		const q = page.url.searchParams.get('month');
		if (q && /^\d{4}-\d{2}$/.test(q)) return q;
		return defaultMonth();
	});

	let localDB = new BudgetLocalDB();
	let loadSeq = 0;

	let budgetData = $state<MonthBudgetDetails | null>(null);
	let transactions = $state<TransactionItemModel[]>([]);
	let queuedTransactions = $state<OfflineTransaction[]>([]);
	let accounts = $state<AccountItem[]>([]);
	let solvency = $state<SolvencyReport | null>(null);

	let isLoading = $state(false);
	let syncing = $state(false);
	let errorMsg = $state('');
	let isOffline = $state(typeof navigator !== 'undefined' ? !navigator.onLine : false);

	// Layout & panel state
	let isMobileDrawerOpen = $state(false);
	let lastUrlPanel = $state<string | null>(null);

	let displayMode = $state<'remaining' | 'spent'>('remaining');
	let activeCategoryFilterId = $state<string | null>(null);
	let activeFocusGroup = $state<string>(FOCUS_ALL);

	// Modals & Drawers
	let coverCategoryId = $state<string | null>(null);
	let isCategoryModalOpen = $state(false);
	let editingCategory = $state<CategoryBudget | null>(null);
	let modalDefaultGroup = $state('Essentials');
	let isCatchUpModalOpen = $state(false);
	let catchUpModalMode = $state<CatchUpMode>('intro');
	let hasAutoPromptedCatchUp = $state(false);

	$effect(() => {
		const panelParam = page.url.searchParams.get('panel');
		if (panelParam !== lastUrlPanel) {
			lastUrlPanel = panelParam;
			if (panelParam === 'transactions') {
				isMobileDrawerOpen = true;
			}
		}
	});

	async function loadWorkspace(month: string, silent = false) {
		const seq = ++loadSeq;
		await auth.ready();
		if (!auth.isAuthenticated) {
			void goto('/login');
			return;
		}
		if (!silent) {
			isLoading = true;
		}
		errorMsg = '';

		try {
			const pending = await localDB.getPendingQueue();
			if (seq !== loadSeq) return;
			queuedTransactions = pending;

			if (typeof navigator !== 'undefined' && !navigator.onLine) {
				isOffline = true;
				const [cachedAccts, cachedCats] = await Promise.all([
					localDB.getCachedAccounts(),
					localDB.getCachedCategories()
				]);
				if (seq !== loadSeq) return;
				accounts = cachedAccts.map((a) => ({
					id: a.id,
					name: a.name,
					type: a.type as AccountItem['type'],
					currentBalance: a.currentBalance,
					isOnBudget: a.isOnBudget,
					cardMode: (a.cardMode as AccountItem['cardMode']) ?? 'PAY_IN_FULL',
					aprPercentage: 0,
					externalAccountId: null,
					initialDebtBaseline: 0,
					statementDueDay: null,
					debtPayoffCategoryId: null
				}));
				if (!budgetData && cachedCats.length > 0) {
					budgetData = {
						summary: {
							yearMonth: `${month}-01`,
							totalIncomeExpected: 0,
							totalAllocated: 0,
							readyToAssign: 0,
							totalSpentCleared: 0,
							totalSpentPending: 0,
							isUnbalanced: false
						},
						categories: cachedCats.map((c, idx) => ({
							id: c.id,
							name: c.name,
							groupName: c.groupName,
							rolloverMode: 'SWEEP',
							sweepTargetCategoryId: null,
							targetAmount: null,
							targetDate: null,
							targetCadence: 'NONE',
							sortOrder: idx,
							originalPlanned: 0,
							effectiveTarget: 0,
							spentCleared: 0,
							spentPending: 0,
							remaining: c.remaining
						})),
						incomes: []
					};
				}
				return;
			}

			isOffline = false;
			const [budgetRes, txRes, acctsRes, solvRes] = await Promise.all([
				fetchMonthBudget(month),
				fetchTransactions({ month, limit: 200 }).catch(() => ({ items: [], total: 0 })),
				fetchAccounts().catch(() => []),
				fetchSolvencyReport().catch(() => null)
			]);
			if (seq !== loadSeq) return;

			budgetData = budgetRes;
			transactions = txRes.items;
			accounts = acctsRes;
			solvency = solvRes;

			if (!hasAutoPromptedCatchUp && !silent) {
				hasAutoPromptedCatchUp = true;
				const loadedAccountById = new Map(acctsRes.map((a) => [a.id, a]));
				if (txRes.items.some((tx) => isCatchUpTransaction(tx, loadedAccountById))) {
					catchUpModalMode = 'intro';
					isCatchUpModalOpen = true;
				}
			}

			await Promise.all([
				localDB.cacheAccounts(
					acctsRes.map((a) => ({
						id: a.id,
						name: a.name,
						type: a.type,
						currentBalance: a.currentBalance,
						isOnBudget: a.isOnBudget,
						cardMode: a.cardMode
					}))
				),
				localDB.cacheCategories(
					budgetRes.categories.map((c) => ({
						id: c.id,
						name: c.name,
						groupName: c.groupName,
						remaining: c.remaining
					}))
				)
			]);
		} catch (err: any) {
			if (seq !== loadSeq) return;
			errorMsg = err?.message || 'Could not load budget workspace.';
		} finally {
			if (seq === loadSeq) {
				isLoading = false;
			}
		}
	}

	$effect(() => {
		const version = demo.dataVersion;
		void loadWorkspace(currentMonth, version > 0);
	});

	onMount(() => {
		const stopListening = registerOnlineSyncListener(() => {
			void loadWorkspace(currentMonth, true);
		});
		const onOffline = () => {
			isOffline = true;
		};
		window.addEventListener('offline', onOffline);
		return () => {
			stopListening();
			window.removeEventListener('offline', onOffline);
		};
	});

	let accountById = $derived(new Map(accounts.map((a) => [a.id, a])));

	let groupedCategories = $derived.by(() => {
		const groups = new Map<string, CategoryBudget[]>();
		for (const cat of budgetData?.categories ?? []) {
			const list = groups.get(cat.groupName) ?? [];
			list.push(cat);
			groups.set(cat.groupName, list);
		}
		return Array.from(groups.entries()).map(([groupName, cats], idx) => ({
			groupName,
			cats,
			color: getGroupColor(groupName, idx)
		}));
	});

	let focusGroupNames = $derived(groupedCategories.map((g) => g.groupName));

	let effectiveFocusGroup = $derived(
		activeFocusGroup === FOCUS_ALL ||
			activeFocusGroup === FOCUS_INCOME ||
			focusGroupNames.includes(activeFocusGroup)
			? activeFocusGroup
			: FOCUS_ALL
	);

	let visibleGroupedCategories = $derived.by(() => {
		if (effectiveFocusGroup === FOCUS_ALL) return groupedCategories;
		if (effectiveFocusGroup === FOCUS_INCOME) return [];
		return groupedCategories.filter((g) => g.groupName === effectiveFocusGroup);
	});

	let transactionsByCategory = $derived.by(() => {
		const map = new Map<string, InlineActivityItem[]>();
		const pushItem = (catId: string, item: InlineActivityItem) => {
			const list = map.get(catId) ?? [];
			list.push(item);
			map.set(catId, list);
		};

		for (const tx of transactions) {
			if (tx.isTransfer) continue;
			const acct = accountById.get(tx.accountId);
			if (acct && !acct.isOnBudget) continue;
			const merchant = tx.merchantClean?.trim() || tx.merchantRaw;
			const accountName = tx.accountName || acct?.name || 'Account';

			if (tx.splits && tx.splits.length > 0) {
				for (const split of tx.splits) {
					if (!split.categoryId) continue;
					pushItem(split.categoryId, {
						id: `${tx.id}:${split.id}`,
						date: tx.transactionDate,
						merchant,
						accountName,
						amount: split.amount,
						status: tx.status
					});
				}
			} else if (tx.categoryId) {
				pushItem(tx.categoryId, {
					id: tx.id,
					date: tx.transactionDate,
					merchant,
					accountName,
					amount: tx.amount,
					status: tx.status
				});
			}
		}

		for (const q of queuedTransactions) {
			if (!q.transactionDate.startsWith(currentMonth) || !q.categoryId) continue;
			const acct = accountById.get(q.accountId);
			if (acct && !acct.isOnBudget) continue;
			pushItem(q.categoryId, {
				id: q.localId,
				date: q.transactionDate,
				merchant: q.merchantRaw,
				accountName: acct?.name || 'Offline',
				amount: q.amount,
				status: 'PENDING'
			});
		}

		return map;
	});

	let inflowTransactions = $derived.by(() => {
		const items: InlineActivityItem[] = [];
		for (const tx of transactions) {
			if (tx.amount <= 0 || tx.isTransfer) continue;
			const acct = accountById.get(tx.accountId);
			if (acct && !acct.isOnBudget) continue;
			items.push({
				id: tx.id,
				date: tx.transactionDate,
				merchant: tx.merchantClean?.trim() || tx.merchantRaw,
				accountName: tx.accountName || acct?.name || 'Account',
				amount: tx.amount,
				status: tx.status
			});
		}
		for (const q of queuedTransactions) {
			if (!q.transactionDate.startsWith(currentMonth) || q.amount <= 0) continue;
			const acct = accountById.get(q.accountId);
			if (acct && !acct.isOnBudget) continue;
			items.push({
				id: q.localId,
				date: q.transactionDate,
				merchant: q.merchantRaw,
				accountName: acct?.name || 'Offline',
				amount: q.amount,
				status: 'PENDING'
			});
		}
		return items;
	});

	let overspentCategory = $derived.by(() => {
		if (!coverCategoryId || !budgetData) return null;
		const found = budgetData.categories.find((c) => c.id === coverCategoryId);
		if (!found) return null;
		return { id: found.id, name: found.name, remaining: found.remaining };
	});

	let availableDonors = $derived.by(() => {
		if (!budgetData) return [];
		return budgetData.categories
			.filter((c) => c.id !== coverCategoryId && c.remaining > 0)
			.map((c) => ({ id: c.id, name: c.name, available: c.remaining }));
	});

	let receivedInflows = $derived.by(() => {
		let total = 0;
		for (const tx of transactions) {
			if (tx.amount > 0 && !tx.isTransfer) {
				const acct = accountById.get(tx.accountId);
				if (!acct || acct.isOnBudget) {
					total += tx.amount;
				}
			}
		}
		for (const q of queuedTransactions) {
			if (q.transactionDate.startsWith(currentMonth) && q.amount > 0) {
				const acct = accountById.get(q.accountId);
				if (!acct || acct.isOnBudget) {
					total += q.amount;
				}
			}
		}
		return total;
	});

	let toReviewCount = $derived.by(() => {
		const offlineMapped = mapOfflineQueuedToTransactions(queuedTransactions, currentMonth);
		let count = 0;
		for (const tx of [...offlineMapped, ...transactions]) {
			if (isToReviewTransaction(tx, accountById)) {
				count++;
			}
		}
		return count;
	});

	let catchUpItems = $derived(
		transactions.filter((tx) => isCatchUpTransaction(tx, accountById))
	);

	let modalCatchUpItems = $derived.by(() =>
		catchUpItems.length > 0
			? catchUpItems
			: transactions.filter(
					(tx) =>
						isToReviewTransaction(tx, accountById) &&
						isCategorizableTransaction(tx, accountById)
				)
	);

	function fmtMoney(n: number): string {
		return Math.abs(n).toLocaleString('en-US', {
			minimumFractionDigits: 2,
			maximumFractionDigits: 2
		});
	}

	function handleMonthChange(nextMonth: string) {
		void goto(`/budget?month=${nextMonth}`, {
			replaceState: false,
			keepFocus: true,
			noScroll: true
		});
	}

	function openEditCategory(catId: string) {
		const found = budgetData?.categories.find((c) => c.id === catId) ?? null;
		editingCategory = found;
		modalDefaultGroup = found?.groupName ?? 'Essentials';
		isCategoryModalOpen = true;
	}

	function openNewCategoryInGroup(groupName: string) {
		editingCategory = null;
		modalDefaultGroup = groupName;
		isCategoryModalOpen = true;
	}

	async function handleUpdatePlanned(catId: string, newPlanned: number) {
		await updateMonthlyPlanned(catId, currentMonth, newPlanned);
		await loadWorkspace(currentMonth, true);
	}

	async function handleCategorizeTransaction(txId: string, categoryId: string | null) {
		await updateTransaction(
			txId,
			categoryId ? { categoryId } : { clearCategory: true }
		);
		await loadWorkspace(currentMonth, true);
	}

	async function handleUpdateTransaction(txId: string, payload: UpdateTransactionPayload) {
		await updateTransaction(txId, payload);
		await loadWorkspace(currentMonth, true);
	}

	async function handleCreateTransaction(payload: {
		accountId: string;
		amount: number;
		merchantRaw: string;
		categoryId: string | null;
		transactionDate: string;
	}) {
		if (typeof navigator !== 'undefined' && !navigator.onLine) {
			const offlineTx: OfflineTransaction = {
				localId: `offline-${crypto.randomUUID()}`,
				accountId: payload.accountId,
				amount: payload.amount,
				merchantRaw: payload.merchantRaw,
				categoryId: payload.categoryId,
				transactionDate: payload.transactionDate,
				syncStatus: 'QUEUED',
				createdAt: Date.now()
			};
			await localDB.queueTransaction(offlineTx);
			if (payload.categoryId && budgetData) {
				budgetData = {
					...budgetData,
					categories: budgetData.categories.map((c) =>
						c.id === payload.categoryId
							? { ...c, remaining: c.remaining + payload.amount }
							: c
					)
				};
				await localDB.cacheCategories(
					budgetData.categories.map((c) => ({
						id: c.id,
						name: c.name,
						groupName: c.groupName,
						remaining: c.remaining
					}))
				);
			}
			await requestBackgroundSync();
			queuedTransactions = await localDB.getPendingQueue();
			return;
		}

		await createTransaction({
			accountId: payload.accountId,
			totalAmount: payload.amount,
			merchantRaw: payload.merchantRaw,
			categoryId: payload.categoryId,
			transactionDate: payload.transactionDate
		});
		await loadWorkspace(currentMonth, true);
	}

	async function handleDeleteTransaction(txId: string) {
		await deleteTransaction(txId);
		await loadWorkspace(currentMonth, true);
	}

	async function handleSyncNow() {
		syncing = true;
		try {
			await flushOfflineQueue(localDB);
			if (typeof navigator === 'undefined' || navigator.onLine) {
				await demo.triggerSyncAndPoll();
			}
			await loadWorkspace(currentMonth, true);
		} catch (err: any) {
			errorMsg = err?.message || 'Sync failed.';
		} finally {
			syncing = false;
		}
	}

	function handleFilterCategoryTransactions(catId: string) {
		activeCategoryFilterId = catId;
		if (typeof window !== 'undefined' && window.innerWidth < 1024) {
			isMobileDrawerOpen = true;
		}
	}

	function openCatchUpStack() {
		if (modalCatchUpItems.length > 0) {
			catchUpModalMode = 'stack';
			isCatchUpModalOpen = true;
		} else {
			isMobileDrawerOpen = true;
		}
	}
</script>

<BudgetHeader
	{currentMonth}
	summary={budgetData?.summary ?? null}
	{solvency}
	accountCount={accounts.length}
	{toReviewCount}
	{isOffline}
	queuedCount={queuedTransactions.length}
	syncing={syncing || demo.syncStatus === 'running' || demo.syncStatus === 'retrying'}
	isDrawerOpen={isMobileDrawerOpen}
	onSyncNow={handleSyncNow}
	onMonthChange={handleMonthChange}
	onOpenCatchUp={openCatchUpStack}
	onToggleTransactionsDrawer={() => (isMobileDrawerOpen = !isMobileDrawerOpen)}
	onSignOut={() => {
		demo.resetOnLogout();
		void auth.logout();
	}}
/>

<div class="max-w-[1440px] mx-auto px-4 sm:px-6 py-5">
	<!-- Hybrid 12-Column Split Grid: 8 cols Budget | 4 cols Sticky Transactions (lg+) -->
	<div class="grid grid-cols-1 lg:grid-cols-12 gap-6 items-start">
		<!-- Left/Main Column: Budget Canvas -->
		<div class="lg:col-span-8 space-y-5 min-w-0">
			<div class="flex flex-wrap items-center justify-between gap-3">
				<div class="flex flex-wrap items-center gap-3 text-xs text-ink-secondary">
					{#if budgetData?.summary}
						<span>
							Planned:
							<strong class="font-mono text-ink-primary">
								${fmtMoney(budgetData.summary.totalAllocated)}
							</strong>
						</span>
						<span>
							Spent:
							<strong class="font-mono text-ink-primary">
								${fmtMoney(budgetData.summary.totalSpentCleared + budgetData.summary.totalSpentPending)}
							</strong>
						</span>
						{#if budgetData.summary.isUnbalanced}
							<span class="rounded-full bg-status-danger-bg text-status-danger px-2 py-0.5 text-[11px] font-semibold">
								Overspent Categories
							</span>
						{/if}
					{/if}
				</div>

				<button
					type="button"
					onclick={() => openNewCategoryInGroup('Essentials')}
					class="rounded-lg bg-accent-primary text-accent-primary-text px-3 py-1.5 text-xs font-semibold hover:opacity-90 transition-opacity"
				>
					+ Category
				</button>
			</div>

			{#if errorMsg}
				<div class="rounded-xl border border-status-danger bg-status-danger-bg p-4 text-xs text-status-danger">
					{errorMsg}
				</div>
			{/if}

			{#if isLoading && !budgetData}
				<div class="rounded-2xl border border-border bg-surface p-8 text-center text-sm text-ink-secondary">
					Loading {currentMonth} budget...
				</div>
			{:else}
				<CategoryFocusPills
					groups={focusGroupNames}
					selected={effectiveFocusGroup}
					onSelect={(value) => (activeFocusGroup = value)}
				/>

				{#if effectiveFocusGroup === FOCUS_ALL || effectiveFocusGroup === FOCUS_INCOME}
					<IncomeCard
						yearMonth={`${currentMonth}-01`}
						incomes={budgetData?.incomes ?? []}
						receivedAmount={receivedInflows}
						{inflowTransactions}
						onChanged={() => void loadWorkspace(currentMonth, true)}
					/>
				{/if}

				{#if effectiveFocusGroup !== FOCUS_INCOME}
					{#if groupedCategories.length === 0}
						<div class="rounded-2xl border border-border bg-surface p-8 text-center space-y-3">
							<p class="text-sm font-medium text-ink-primary">No categories yet</p>
							<p class="text-xs text-ink-secondary">
								Add your first budget category or planned income to start assigning every dollar.
							</p>
						</div>
					{:else}
						<div class="space-y-5">
							{#each visibleGroupedCategories as group (group.groupName)}
								<CategoryGroupCard
									groupName={group.groupName}
									color={group.color}
									categories={group.cats}
									{displayMode}
									{transactionsByCategory}
									onToggleDisplayMode={() =>
										(displayMode = displayMode === 'remaining' ? 'spent' : 'remaining')}
									onCoverClick={(id) => (coverCategoryId = id)}
									onEditClick={openEditCategory}
									onUpdatePlanned={handleUpdatePlanned}
									onFilterTransactions={handleFilterCategoryTransactions}
									onAddCategoryInGroup={openNewCategoryInGroup}
								/>
							{/each}
						</div>
					{/if}
				{/if}
			{/if}
		</div>

		<!-- Right Column (Desktop lg+): Dedicated Sticky Transactions Panel -->
		<aside
			aria-label="Transactions"
			data-testid="desktop-transactions-panel"
			class="hidden lg:flex lg:col-span-4 lg:sticky lg:top-[4.75rem] flex-col rounded-2xl border border-border bg-surface shadow-sm overflow-hidden lg:h-[calc(100dvh-6rem)]"
		>
			<div class="px-4 py-3 border-b border-border bg-subtle flex items-center justify-between shrink-0">
				<h2 class="text-xs font-bold uppercase tracking-wider text-ink-primary">Transactions</h2>
				{#if toReviewCount > 0}
					<span class="inline-flex items-center justify-center min-w-[20px] h-5 px-1.5 rounded-full text-[11px] font-bold bg-accent-pop text-accent-primary-text">
						{toReviewCount}
					</span>
				{/if}
			</div>

			<div class="flex-1 min-h-0 flex flex-col">
				<TransactionsPanel
					{currentMonth}
					{transactions}
					{queuedTransactions}
					{accounts}
					categories={budgetData?.categories ?? []}
					{isOffline}
					syncing={syncing || demo.syncStatus === 'running' || demo.syncStatus === 'retrying'}
					{activeCategoryFilterId}
					onClearCategoryFilter={() => (activeCategoryFilterId = null)}
					onCreateTransaction={handleCreateTransaction}
					onCategorizeTransaction={handleCategorizeTransaction}
					onUpdateTransaction={handleUpdateTransaction}
					onDeleteTransaction={handleDeleteTransaction}
					onSyncNow={handleSyncNow}
				/>
			</div>
		</aside>
	</div>
</div>

<TransactionsDrawer
	isOpen={isMobileDrawerOpen}
	title="Transactions"
	badgeCount={toReviewCount}
	onClose={() => (isMobileDrawerOpen = false)}
>
	<TransactionsPanel
		{currentMonth}
		{transactions}
		{queuedTransactions}
		{accounts}
		categories={budgetData?.categories ?? []}
		{isOffline}
		syncing={syncing || demo.syncStatus === 'running' || demo.syncStatus === 'retrying'}
		{activeCategoryFilterId}
		onClearCategoryFilter={() => (activeCategoryFilterId = null)}
		onCreateTransaction={handleCreateTransaction}
		onCategorizeTransaction={handleCategorizeTransaction}
		onUpdateTransaction={handleUpdateTransaction}
		onDeleteTransaction={handleDeleteTransaction}
		onSyncNow={handleSyncNow}
	/>
</TransactionsDrawer>

<CoverDrawer
	isOpen={coverCategoryId !== null}
	yearMonth={`${currentMonth}-01`}
	{overspentCategory}
	{availableDonors}
	onClose={() => (coverCategoryId = null)}
	onSuccess={() => {
		coverCategoryId = null;
		void loadWorkspace(currentMonth, true);
	}}
/>

<CategoryModal
	isOpen={isCategoryModalOpen}
	{editingCategory}
	allCategories={budgetData?.categories ?? []}
	defaultGroupName={modalDefaultGroup}
	yearMonth={currentMonth}
	onClose={() => (isCategoryModalOpen = false)}
	onSuccess={() => {
		isCategoryModalOpen = false;
		void loadWorkspace(currentMonth, true);
	}}
/>

<CatchUpModal
	isOpen={isCatchUpModalOpen}
	initialMode={catchUpModalMode}
	items={modalCatchUpItems}
	allTransactions={transactions}
	{accounts}
	categories={budgetData?.categories ?? []}
	onCategorizeTransaction={handleCategorizeTransaction}
	onClose={() => (isCatchUpModalOpen = false)}
/>
