<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/stores/auth.svelte';
	import {
		createTransaction,
		fetchAccounts,
		fetchSolvencyReport,
		fetchTransactions
	} from '$lib/api/transactions';
	import { fetchMonthBudget } from '$lib/api/budget';
	import TransactionItem from '$lib/components/transactions/TransactionItem.svelte';
	import { BudgetLocalDB, type OfflineTransaction } from '$lib/offline/db';
	import { flushOfflineQueue, registerOnlineSyncListener, requestBackgroundSync } from '$lib/offline/sync';
	import type { AccountItem, SolvencyReport, TransactionItemModel } from '$lib/types/transaction';

	let localDB = new BudgetLocalDB();
	let transactions = $state<TransactionItemModel[]>([]);
	let queuedTransactions = $state<OfflineTransaction[]>([]);
	let accounts = $state<AccountItem[]>([]);
	let categories = $state<Array<{ id: string; name: string; groupName: string; remaining: number }>>([]);
	let solvency = $state<SolvencyReport | null>(null);

	let loading = $state(true);
	let syncing = $state(false);
	let errorMessage = $state<string | null>(null);
	let isOffline = $state(typeof navigator !== 'undefined' ? !navigator.onLine : false);

	let selectedAccountId = $state('');
	let selectedStatus = $state('');

	// Quick-add form state
	let formAccountId = $state('');
	let formCategoryId = $state('');
	let formMerchant = $state('');
	let formAmount = $state('');
	let formDate = $state(new Date().toISOString().slice(0, 10));
	let submitting = $state(false);

	let accountMap = $derived(new Map(accounts.map((a) => [a.id, a.name])));
	let categoryMap = $derived(new Map(categories.map((c) => [c.id, c.name])));

	let combinedItems = $derived.by(() => {
		const offlineMapped: TransactionItemModel[] = queuedTransactions.map((q) => ({
			id: q.localId,
			accountId: q.accountId,
			origin: 'MANUAL_OFFLINE',
			status: 'PENDING',
			amount: q.amount,
			merchantRaw: q.merchantRaw,
			categoryId: q.categoryId,
			transactionDate: q.transactionDate,
			isCleared: false,
			syncStatus: q.syncStatus
		}));
		return [...offlineMapped, ...transactions];
	});

	async function loadData() {
		await auth.ready();
		if (!auth.isAuthenticated) {
			void goto('/login');
			return;
		}
		loading = true;
		errorMessage = null;
		try {
			queuedTransactions = await localDB.getPendingQueue();

			if (typeof navigator !== 'undefined' && !navigator.onLine) {
				isOffline = true;
				const cachedAccts = await localDB.getCachedAccounts();
				accounts = cachedAccts.map((a) => ({
					id: a.id,
					name: a.name,
					type: a.type as AccountItem['type'],
					currentBalance: a.currentBalance,
					isOnBudget: a.isOnBudget,
					cardMode: (a.cardMode as AccountItem['cardMode']) ?? 'PAY_IN_FULL',
					aprPercentage: 0
				}));
				categories = await localDB.getCachedCategories();
				if (!formAccountId && accounts.length > 0) formAccountId = accounts[0].id;
				return;
			}

			isOffline = false;
			const currentMonth = new Date().toISOString().slice(0, 7);
			const [txRes, acctsRes, budgetRes, solvencyRes] = await Promise.all([
				fetchTransactions({
					accountId: selectedAccountId || undefined,
					status: selectedStatus || undefined
				}),
				fetchAccounts(),
				fetchMonthBudget(currentMonth).catch(() => null),
				fetchSolvencyReport().catch(() => null)
			]);

			transactions = txRes.items;
			accounts = acctsRes;
			solvency = solvencyRes;

			if (!formAccountId && accounts.length > 0) {
				formAccountId = accounts[0].id;
			}

			await localDB.cacheAccounts(
				accounts.map((a) => ({
					id: a.id,
					name: a.name,
					type: a.type,
					currentBalance: a.currentBalance,
					isOnBudget: a.isOnBudget,
					cardMode: a.cardMode
				}))
			);

			if (budgetRes) {
				categories = budgetRes.categories.map((c) => ({
					id: c.id,
					name: c.name,
					groupName: c.groupName,
					remaining: c.remaining
				}));
				await localDB.cacheCategories(categories);
			}
		} catch (err) {
			errorMessage = err instanceof Error ? err.message : 'Failed to load transactions';
		} finally {
			loading = false;
		}
	}

	async function handleSyncNow() {
		syncing = true;
		try {
			await flushOfflineQueue(localDB);
			await loadData();
		} catch (err) {
			errorMessage = err instanceof Error ? err.message : 'Sync failed';
		} finally {
			syncing = false;
		}
	}

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		const parsedAmount = Number(formAmount);
		if (!formAccountId || !formMerchant.trim() || !Number.isFinite(parsedAmount) || parsedAmount === 0) {
			return;
		}

		submitting = true;
		errorMessage = null;
		const normalizedAmount = parsedAmount > 0 ? -Math.abs(parsedAmount) : parsedAmount;

		try {
			if (typeof navigator !== 'undefined' && !navigator.onLine) {
				const offlineTx: OfflineTransaction = {
					localId: `offline-${crypto.randomUUID()}`,
					accountId: formAccountId,
					amount: normalizedAmount,
					merchantRaw: formMerchant.trim(),
					categoryId: formCategoryId || null,
					transactionDate: formDate,
					syncStatus: 'QUEUED',
					createdAt: Date.now()
				};
				await localDB.queueTransaction(offlineTx);
				if (formCategoryId) {
					categories = categories.map((c) =>
						c.id === formCategoryId ? { ...c, remaining: c.remaining + normalizedAmount } : c
					);
					await localDB.cacheCategories(categories);
				}
				await requestBackgroundSync();
				queuedTransactions = await localDB.getPendingQueue();
			} else {
				await createTransaction({
					accountId: formAccountId,
					totalAmount: normalizedAmount,
					merchantRaw: formMerchant.trim(),
					categoryId: formCategoryId || null,
					transactionDate: formDate
				});
				await loadData();
			}
			formMerchant = '';
			formAmount = '';
		} catch (err) {
			errorMessage = err instanceof Error ? err.message : 'Failed to record transaction';
		} finally {
			submitting = false;
		}
	}

	onMount(() => {
		void loadData();
		const stopListening = registerOnlineSyncListener(() => {
			void loadData();
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
</script>

<div class="max-w-5xl mx-auto p-4 sm:p-6 space-y-6">
	<div class="flex flex-wrap items-center justify-between gap-4">
		<div>
			<h2 class="text-xl font-bold text-slate-100">Transactions</h2>
			<p class="text-xs text-slate-400">Account activity, split ledger, and offline queue</p>
		</div>

		<div class="flex items-center gap-2">
			{#if isOffline}
				<span class="text-xs px-2.5 py-1 rounded-full bg-amber-500/15 text-amber-300 border border-amber-500/30">
					Offline Mode
				</span>
			{/if}
			{#if queuedTransactions.length > 0}
				<button
					type="button"
					onclick={handleSyncNow}
					disabled={syncing || isOffline}
					class="text-xs px-3 py-1.5 rounded-md bg-sky-600 hover:bg-sky-500 disabled:opacity-50 text-white font-medium transition-colors"
				>
					{syncing ? 'Syncing...' : `Sync Queued (${queuedTransactions.length})`}
				</button>
			{/if}
		</div>
	</div>

	{#if solvency}
		<div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
			<div class="bg-slate-900 border border-slate-800 rounded-lg p-3">
				<p class="text-xs text-slate-400">Liquid Cash (On-Budget)</p>
				<p class="text-lg font-mono font-semibold text-emerald-400">${solvency.totalLiquidCash.toFixed(2)}</p>
			</div>
			<div class="bg-slate-900 border border-slate-800 rounded-lg p-3">
				<p class="text-xs text-slate-400">Pay-In-Full CC Debt</p>
				<p class="text-lg font-mono font-semibold text-slate-200">${solvency.totalCreditCardDebt.toFixed(2)}</p>
			</div>
			<div class="bg-slate-900 border border-slate-800 rounded-lg p-3">
				<p class="text-xs text-slate-400">Solvency Buffer</p>
				<p class="text-lg font-mono font-semibold {solvency.isFloatingDebt ? 'text-rose-400' : 'text-emerald-400'}">
					${solvency.solvencyBuffer.toFixed(2)}
					{#if solvency.isFloatingDebt}
						<span class="text-xs font-sans font-normal text-rose-300 ml-1">(Floating Debt)</span>
					{/if}
				</p>
			</div>
		</div>
	{/if}

	<form
		onsubmit={handleSubmit}
		class="bg-slate-900 border border-slate-800 rounded-lg p-4 grid grid-cols-1 sm:grid-cols-5 gap-3 items-end"
	>
		<div>
			<label class="block text-xs text-slate-400 mb-1" for="tx-account">Account</label>
			<select
				id="tx-account"
				bind:value={formAccountId}
				class="w-full bg-slate-950 border border-slate-700 rounded-md px-2.5 py-1.5 text-sm text-slate-100"
				required
			>
				{#each accounts as acct}
					<option value={acct.id}>{acct.name}</option>
				{/each}
			</select>
		</div>

		<div>
			<label class="block text-xs text-slate-400 mb-1" for="tx-merchant">Merchant</label>
			<input
				id="tx-merchant"
				type="text"
				bind:value={formMerchant}
				placeholder="Grocery Store"
				class="w-full bg-slate-950 border border-slate-700 rounded-md px-2.5 py-1.5 text-sm text-slate-100"
				required
			/>
		</div>

		<div>
			<label class="block text-xs text-slate-400 mb-1" for="tx-amount">Amount ($)</label>
			<input
				id="tx-amount"
				type="number"
				step="0.01"
				bind:value={formAmount}
				placeholder="25.00"
				class="w-full bg-slate-950 border border-slate-700 rounded-md px-2.5 py-1.5 text-sm text-slate-100"
				required
			/>
		</div>

		<div>
			<label class="block text-xs text-slate-400 mb-1" for="tx-category">Category</label>
			<select
				id="tx-category"
				bind:value={formCategoryId}
				class="w-full bg-slate-950 border border-slate-700 rounded-md px-2.5 py-1.5 text-sm text-slate-100"
			>
				<option value="">Uncategorized</option>
				{#each categories as cat}
					<option value={cat.id}>{cat.name}</option>
				{/each}
			</select>
		</div>

		<button
			type="submit"
			disabled={submitting || !formAccountId}
			class="bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 text-white text-sm font-medium px-4 py-1.5 rounded-md transition-colors"
		>
			{submitting ? 'Saving...' : isOffline ? 'Queue Offline' : 'Add Expense'}
		</button>
	</form>

	<div class="flex flex-wrap items-center gap-3">
		<select
			bind:value={selectedAccountId}
			onchange={() => void loadData()}
			class="bg-slate-900 border border-slate-800 rounded-md px-3 py-1.5 text-xs text-slate-200"
		>
			<option value="">All Accounts</option>
			{#each accounts as acct}
				<option value={acct.id}>{acct.name}</option>
			{/each}
		</select>

		<select
			bind:value={selectedStatus}
			onchange={() => void loadData()}
			class="bg-slate-900 border border-slate-800 rounded-md px-3 py-1.5 text-xs text-slate-200"
		>
			<option value="">All Statuses</option>
			<option value="PENDING">Pending</option>
			<option value="POSTED">Posted</option>
		</select>
	</div>

	{#if errorMessage}
		<div class="bg-rose-500/10 border border-rose-500/30 text-rose-300 text-xs px-3 py-2 rounded-md">
			{errorMessage}
		</div>
	{/if}

	{#if loading}
		<div class="text-sm text-slate-400 py-8 text-center">Loading transactions...</div>
	{:else if combinedItems.length === 0}
		<div class="text-sm text-slate-400 py-8 text-center border border-dashed border-slate-800 rounded-lg">
			No transactions found.
		</div>
	{:else}
		<div class="space-y-2">
			{#each combinedItems as item (item.id)}
				<TransactionItem
					transaction={item}
					accountName={accountMap.get(item.accountId)}
					categoryName={item.categoryId ? categoryMap.get(item.categoryId) : undefined}
				/>
			{/each}
		</div>
	{/if}
</div>
