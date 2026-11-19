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
	import CategoryGroupCard from '$lib/components/budget/CategoryGroupCard.svelte';
	import CategoryModal from '$lib/components/budget/CategoryModal.svelte';
	import CoverDrawer from '$lib/components/budget/CoverDrawer.svelte';
	import IncomeCard from '$lib/components/budget/IncomeCard.svelte';
	import SummaryPanel from '$lib/components/budget/SummaryPanel.svelte';
	import TransactionsPanel from '$lib/components/budget/TransactionsPanel.svelte';
	import { BudgetLocalDB, type OfflineTransaction } from '$lib/offline/db';
	import {
		flushOfflineQueue,
		registerOnlineSyncListener,
		requestBackgroundSync
	} from '$lib/offline/sync';
	import { auth } from '$lib/stores/auth.svelte';
	import type { CategoryBudget, MonthBudgetDetails } from '$lib/types/budget';
	import type { AccountItem, SolvencyReport, TransactionItemModel } from '$lib/types/transaction';
	import { getGroupColor, groupSlug } from '$lib/utils/budgetColors';

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
	let inspectorTab = $state<'summary' | 'transactions'>('summary');
	let mobileView = $state<'budget' | 'summary' | 'transactions'>('budget');
	let lastUrlPanel = $state<string | null>(null);

	let displayMode = $state<'remaining' | 'spent'>('remaining');
	let highlightedGroup = $state<string | null>(null);
	let activeCategoryFilterId = $state<string | null>(null);

	// Modals & Drawers
	let coverCategoryId = $state<string | null>(null);
	let isCategoryModalOpen = $state(false);
	let editingCategory = $state<CategoryBudget | null>(null);
	let modalDefaultGroup = $state('Essentials');

	$effect(() => {
		const panelParam = page.url.searchParams.get('panel');
		if (panelParam !== lastUrlPanel) {
			lastUrlPanel = panelParam;
			if (panelParam === 'transactions') {
				inspectorTab = 'transactions';
				mobileView = 'transactions';
			} else if (panelParam === 'summary') {
				inspectorTab = 'summary';
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
		void loadWorkspace(currentMonth);
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

	let newUncategorizedCount = $derived.by(() => {
		let count = 0;
		for (const tx of transactions) {
			if (tx.categoryId !== null || tx.isTransfer || (tx.splits && tx.splits.length > 0)) {
				continue;
			}
			const acct = accountById.get(tx.accountId);
			if (acct && !acct.isOnBudget) continue;
			count++;
		}
		for (const q of queuedTransactions) {
			if (!q.transactionDate.startsWith(currentMonth) || q.categoryId !== null) continue;
			const acct = accountById.get(q.accountId);
			if (acct && !acct.isOnBudget) continue;
			count++;
		}
		return count;
	});

	function handleMonthChange(nextMonth: string) {
		const panelQuery = inspectorTab === 'transactions' ? '&panel=transactions' : '';
		void goto(`/budget?month=${nextMonth}${panelQuery}`, {
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
			await loadWorkspace(currentMonth, true);
		} catch (err: any) {
			errorMsg = err?.message || 'Sync failed.';
		} finally {
			syncing = false;
		}
	}

	function handleSelectSummaryGroup(groupName: string | null) {
		highlightedGroup = groupName;
		if (groupName) {
			if (mobileView !== 'budget') {
				mobileView = 'budget';
			}
			queueMicrotask(() => {
				const el = document.getElementById(groupSlug(groupName));
				el?.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
			});
		}
	}

	function handleFilterCategoryTransactions(catId: string) {
		activeCategoryFilterId = catId;
		inspectorTab = 'transactions';
		mobileView = 'transactions';
	}

	function switchInspectorTab(tab: 'summary' | 'transactions') {
		inspectorTab = tab;
		if (mobileView !== 'budget') {
			mobileView = tab;
		}
	}
</script>

<div class="max-w-[1360px] mx-auto p-4 sm:p-6">
	<!-- Mobile/Tablet Switcher (< lg) -->
	<div class="lg:hidden mb-4">
		<div
			role="tablist"
			aria-label="Workspace sections"
			class="grid grid-cols-3 rounded-xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 p-1 shadow-xs"
		>
			<button
				type="button"
				role="tab"
				aria-selected={mobileView === 'budget'}
				onclick={() => (mobileView = 'budget')}
				class="rounded-lg py-2 text-xs font-semibold transition-colors {mobileView === 'budget'
					? 'bg-emerald-600 text-white shadow-xs'
					: 'text-slate-600 dark:text-slate-300'}"
			>
				Budget
			</button>
			<button
				type="button"
				role="tab"
				aria-selected={mobileView === 'summary'}
				onclick={() => {
					mobileView = 'summary';
					inspectorTab = 'summary';
				}}
				class="rounded-lg py-2 text-xs font-semibold transition-colors {mobileView === 'summary'
					? 'bg-emerald-600 text-white shadow-xs'
					: 'text-slate-600 dark:text-slate-300'}"
			>
				Summary
			</button>
			<button
				type="button"
				role="tab"
				aria-selected={mobileView === 'transactions'}
				onclick={() => {
					mobileView = 'transactions';
					inspectorTab = 'transactions';
				}}
				class="rounded-lg py-2 text-xs font-semibold transition-colors inline-flex items-center justify-center gap-1.5 {mobileView ===
				'transactions'
					? 'bg-emerald-600 text-white shadow-xs'
					: 'text-slate-600 dark:text-slate-300'}"
			>
				{#if newUncategorizedCount > 0}
					<span
						class="px-1.5 py-0.2 rounded-full text-[10px] font-bold {mobileView === 'transactions'
							? 'bg-white text-emerald-700'
							: 'bg-sky-600 text-white'}"
					>
						{newUncategorizedCount}
					</span>
				{/if}
				<span>Transactions</span>
			</button>
		</div>
	</div>

	<!-- Unified 2-Column Workspace Grid -->
	<div class="grid grid-cols-1 lg:grid-cols-[minmax(0,1fr)_380px] xl:grid-cols-[minmax(0,1fr)_410px] gap-6 items-start">
		<!-- Left Column: Budget Canvas -->
		<div class="space-y-5 min-w-0 {mobileView === 'budget' ? 'block' : 'hidden lg:block'}">
			<BudgetHeader
				{currentMonth}
				summary={budgetData?.summary ?? null}
				{solvency}
				accountCount={accounts.length}
				{isOffline}
				queuedCount={queuedTransactions.length}
				{syncing}
				onSyncNow={handleSyncNow}
				onMonthChange={handleMonthChange}
				onOpenNewCategory={() => openNewCategoryInGroup('Essentials')}
			/>

			{#if errorMsg}
				<div class="rounded-xl border border-rose-200 dark:border-rose-500/30 bg-rose-50 dark:bg-rose-500/10 p-4 text-xs text-rose-700 dark:text-rose-300">
					{errorMsg}
				</div>
			{/if}

			{#if isLoading && !budgetData}
				<div class="rounded-2xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 p-8 text-center text-sm text-slate-500 dark:text-slate-400">
					Loading {currentMonth} budget...
				</div>
			{:else}
				<IncomeCard
					yearMonth={`${currentMonth}-01`}
					incomes={budgetData?.incomes ?? []}
					receivedAmount={receivedInflows}
					onChanged={() => void loadWorkspace(currentMonth, true)}
				/>

				{#if groupedCategories.length === 0}
					<div class="rounded-2xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 p-8 text-center space-y-3">
						<p class="text-sm font-medium text-slate-800 dark:text-slate-200">No categories yet</p>
						<p class="text-xs text-slate-500 dark:text-slate-400">
							Add your first budget category or planned income to start assigning every dollar.
						</p>
					</div>
				{:else}
					<div class="space-y-5">
						{#each groupedCategories as group (group.groupName)}
							<CategoryGroupCard
								groupName={group.groupName}
								color={group.color}
								categories={group.cats}
								{displayMode}
								highlighted={highlightedGroup === group.groupName}
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
		</div>

		<!-- Right Column: Sticky Inspector Panel (Summary | Transactions) -->
		<aside
			aria-label="Budget summary and transactions"
			class="lg:sticky lg:top-6 flex flex-col rounded-2xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 shadow-sm overflow-hidden min-h-[500px] lg:h-[calc(100dvh-6.5rem)] {mobileView ===
			'budget'
				? 'hidden lg:flex'
				: 'flex'}"
		>
			<!-- Segmented Switcher: Summary | Transactions -->
			<div
				role="tablist"
				aria-label="Inspector view"
				class="grid grid-cols-2 p-1.5 gap-1 bg-slate-100/80 dark:bg-slate-950/60 border-b border-slate-200 dark:border-slate-800 shrink-0"
			>
				<button
					type="button"
					role="tab"
					aria-selected={inspectorTab === 'summary'}
					onclick={() => switchInspectorTab('summary')}
					class="relative flex items-center justify-center gap-2 rounded-xl py-2 px-3 text-xs font-semibold transition-all {inspectorTab ===
					'summary'
						? 'bg-white dark:bg-slate-800 text-slate-900 dark:text-white shadow-xs ring-1 ring-slate-200/80 dark:ring-slate-700'
						: 'text-slate-600 dark:text-slate-400 hover:text-slate-900 dark:hover:text-slate-200'}"
				>
					<svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
						<path d="M21.21 15.89A10 10 0 1 1 8 2.83" />
						<path d="M22 12A10 10 0 0 0 12 2v10z" />
					</svg>
					<span>Summary</span>
				</button>

				<button
					type="button"
					role="tab"
					aria-selected={inspectorTab === 'transactions'}
					onclick={() => switchInspectorTab('transactions')}
					class="relative flex items-center justify-center gap-2 rounded-xl py-2 px-3 text-xs font-semibold transition-all {inspectorTab ===
					'transactions'
						? 'bg-white dark:bg-slate-800 text-slate-900 dark:text-white shadow-xs ring-1 ring-slate-200/80 dark:ring-slate-700'
						: 'text-slate-600 dark:text-slate-400 hover:text-slate-900 dark:hover:text-slate-200'}"
				>
					{#if newUncategorizedCount > 0}
						<span
							class="inline-flex items-center justify-center min-w-[20px] h-5 px-1.5 rounded-full text-[11px] font-bold bg-sky-600 text-white"
						>
							{newUncategorizedCount}
						</span>
					{/if}
					<span>Transactions</span>
				</button>
			</div>

			<!-- Active Panel Body -->
			<div class="flex-1 min-h-0 flex flex-col">
				{#if inspectorTab === 'summary'}
					<SummaryPanel
						categories={budgetData?.categories ?? []}
						summary={budgetData?.summary ?? null}
						selectedGroup={highlightedGroup}
						onSelectGroup={handleSelectSummaryGroup}
					/>
				{:else}
					<TransactionsPanel
						{currentMonth}
						{transactions}
						{queuedTransactions}
						{accounts}
						categories={budgetData?.categories ?? []}
						{isOffline}
						{syncing}
						{activeCategoryFilterId}
						onClearCategoryFilter={() => (activeCategoryFilterId = null)}
						onCreateTransaction={handleCreateTransaction}
						onCategorizeTransaction={handleCategorizeTransaction}
						onDeleteTransaction={handleDeleteTransaction}
						onSyncNow={handleSyncNow}
					/>
				{/if}
			</div>
		</aside>
	</div>
</div>

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
