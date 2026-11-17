<script lang="ts">
	import { onMount } from 'svelte';
	import {
		createAccount,
		createTransaction,
		fetchAccounts,
		fetchSolvencyReport,
		fetchTransactions
	} from '$lib/api/transactions';
	import TransactionItem from '$lib/components/transactions/TransactionItem.svelte';
	import { localDB } from '$lib/offline/db';
	import { flushOfflineQueue, registerOnlineSyncListener } from '$lib/offline/sync';
	import type { Account, SolvencyReport, TransactionView } from '$lib/types/transactions';

	let accounts = $state<Account[]>([]);
	let solvency = $state<SolvencyReport | null>(null);
	let transactions = $state<TransactionView[]>([]);
	let queuedCount = $state(0);
	let isLoading = $state(false);
	let errorMsg = $state('');

	let showAddTx = $state(false);
	let txAccountId = $state('');
	let txMerchant = $state('');
	let txAmount = $state('');
	let txDate = $state(new Date().toISOString().slice(0, 10));
	let isSubmitting = $state(false);

	let showAddAccount = $state(false);
	let acctName = $state('');
	let acctType = $state<'checking' | 'savings' | 'credit_card'>('checking');
	let acctBalance = $state('0.00');

	async function loadAll() {
		isLoading = true;
		errorMsg = '';
		try {
			const [accts, sol, txs, pendingQueue] = await Promise.all([
				fetchAccounts().catch(async () => localDB.getCachedAccounts<Account>()),
				fetchSolvencyReport().catch(() => null),
				fetchTransactions().catch(() => [] as TransactionView[]),
				localDB.getPendingQueue()
			]);

			accounts = accts;
			if (accts.length > 0) {
				await localDB.cacheAccounts(accts);
				if (!txAccountId) txAccountId = accts[0].id;
			}
			solvency = sol;
			queuedCount = pendingQueue.length;

			const queuedViews: TransactionView[] = pendingQueue.map((q) => ({
				id: q.localId,
				accountId: q.accountId,
				origin: 'MANUAL_OFFLINE',
				status: 'POSTED',
				amount: q.amount,
				merchantRaw: q.merchantRaw,
				merchantClean: null,
				categoryId: q.categoryId,
				transactionDate: q.transactionDate,
				userNotes: null,
				isCleared: true,
				syncStatus: q.syncStatus
			}));

			transactions = [...queuedViews, ...txs];
		} catch (err: any) {
			errorMsg = err?.message || 'Could not load transactions.';
		} finally {
			isLoading = false;
		}
	}

	onMount(() => {
		loadAll();
		return registerOnlineSyncListener(() => {
			loadAll();
		});
	});

	async function handleAddTransaction(e: SubmitEvent) {
		e.preventDefault();
		const amt = Number(txAmount);
		if (!txAccountId || !txMerchant.trim() || !Number.isFinite(amt) || amt === 0) return;

		isSubmitting = true;
		errorMsg = '';
		try {
			if (typeof navigator !== 'undefined' && !navigator.onLine) {
				await localDB.queueTransaction({
					localId: crypto.randomUUID(),
					accountId: txAccountId,
					amount: amt,
					merchantRaw: txMerchant.trim(),
					categoryId: null,
					transactionDate: txDate,
					syncStatus: 'QUEUED',
					createdAt: Date.now()
				});
			} else {
				try {
					await createTransaction({
						account_id: txAccountId,
						amount: amt,
						merchant_raw: txMerchant.trim(),
						category_id: null,
						transaction_date: txDate
					});
				} catch {
					// Fallback to offline queue if API is unreachable
					await localDB.queueTransaction({
						localId: crypto.randomUUID(),
						accountId: txAccountId,
						amount: amt,
						merchantRaw: txMerchant.trim(),
						categoryId: null,
						transactionDate: txDate,
						syncStatus: 'QUEUED',
						createdAt: Date.now()
					});
				}
			}
			txMerchant = '';
			txAmount = '';
			showAddTx = false;
			await loadAll();
		} finally {
			isSubmitting = false;
		}
	}

	async function handleAddAccount(e: SubmitEvent) {
		e.preventDefault();
		isSubmitting = true;
		try {
			await createAccount({
				name: acctName.trim(),
				type: acctType,
				current_balance: Number(acctBalance) || 0,
				is_on_budget: true
			});
			acctName = '';
			acctBalance = '0.00';
			showAddAccount = false;
			await loadAll();
		} catch (err: any) {
			errorMsg = err?.message || 'Failed to create account.';
		} finally {
			isSubmitting = false;
		}
	}

	async function handleManualSync() {
		try {
			await flushOfflineQueue();
			await loadAll();
		} catch (err: any) {
			errorMsg = err?.message || 'Sync failed.';
		}
	}
</script>

<div class="p-4 sm:p-6 max-w-4xl mx-auto space-y-6">
	<div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4 rounded-2xl border border-slate-800 bg-slate-900 p-5">
		<div>
			<h2 class="text-lg font-bold text-white tracking-tight">Transactions &amp; Accounts</h2>
			<p class="text-xs text-slate-400 mt-0.5">
				Solvency Buffer:
				<span class="font-semibold {solvency?.isFloatingDebt ? 'text-rose-400' : 'text-emerald-400'}">
					${(solvency?.solvencyBuffer ?? 0).toFixed(2)}
				</span>
				{#if queuedCount > 0}
					<span class="ml-2 text-sky-400">• {queuedCount} queued offline</span>
				{/if}
			</p>
		</div>

		<div class="flex flex-wrap items-center gap-2">
			{#if queuedCount > 0}
				<button
					type="button"
					onclick={handleManualSync}
					class="rounded-lg border border-sky-500/40 bg-sky-500/10 px-3 py-1.5 text-xs font-medium text-sky-300 hover:bg-sky-500/20"
				>
					Sync Offline ({queuedCount})
				</button>
			{/if}
			<button
				type="button"
				onclick={() => (showAddAccount = !showAddAccount)}
				class="rounded-lg border border-slate-700 bg-slate-800 px-3 py-1.5 text-xs font-medium text-slate-200 hover:bg-slate-700"
			>
				+ Account
			</button>
			<button
				type="button"
				onclick={() => (showAddTx = !showAddTx)}
				class="rounded-lg bg-emerald-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-emerald-500"
			>
				+ Log Transaction
			</button>
		</div>
	</div>

	{#if errorMsg}
		<div class="rounded-xl border border-rose-500/30 bg-rose-500/10 p-3 text-xs text-rose-300">
			{errorMsg}
		</div>
	{/if}

	{#if showAddAccount}
		<form onsubmit={handleAddAccount} class="rounded-xl border border-slate-800 bg-slate-900 p-4 space-y-3">
			<h3 class="text-xs font-semibold uppercase tracking-wider text-slate-400">New Account</h3>
			<div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
				<input
					type="text"
					placeholder="Account Name (e.g. Checking)"
					bind:value={acctName}
					required
					class="rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white"
				/>
				<select
					bind:value={acctType}
					class="rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white"
				>
					<option value="checking">Checking</option>
					<option value="savings">Savings</option>
					<option value="credit_card">Credit Card</option>
				</select>
				<input
					type="number"
					step="0.01"
					placeholder="Balance"
					bind:value={acctBalance}
					class="rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white"
				/>
			</div>
			<div class="flex justify-end gap-2">
				<button type="button" onclick={() => (showAddAccount = false)} class="text-xs text-slate-400 px-3 py-1.5">
					Cancel
				</button>
				<button type="submit" disabled={isSubmitting} class="rounded-lg bg-emerald-600 px-3 py-1.5 text-xs font-semibold text-white">
					Save Account
				</button>
			</div>
		</form>
	{/if}

	{#if showAddTx}
		<form onsubmit={handleAddTransaction} class="rounded-xl border border-slate-800 bg-slate-900 p-4 space-y-3">
			<h3 class="text-xs font-semibold uppercase tracking-wider text-slate-400">Log Transaction</h3>
			<div class="grid grid-cols-1 sm:grid-cols-4 gap-3">
				<select
					bind:value={txAccountId}
					required
					class="rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white"
				>
					{#each accounts as acct}
						<option value={acct.id}>{acct.name}</option>
					{/each}
				</select>
				<input
					type="text"
					placeholder="Merchant"
					bind:value={txMerchant}
					required
					class="rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white"
				/>
				<input
					type="number"
					step="0.01"
					placeholder="-45.00"
					bind:value={txAmount}
					required
					class="rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white"
				/>
				<input
					type="date"
					bind:value={txDate}
					required
					class="rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white"
				/>
			</div>
			<div class="flex justify-end gap-2">
				<button type="button" onclick={() => (showAddTx = false)} class="text-xs text-slate-400 px-3 py-1.5">
					Cancel
				</button>
				<button type="submit" disabled={isSubmitting || accounts.length === 0} class="rounded-lg bg-emerald-600 px-3 py-1.5 text-xs font-semibold text-white">
					Save Transaction
				</button>
			</div>
		</form>
	{/if}

	{#if isLoading && transactions.length === 0}
		<div class="rounded-2xl border border-slate-800 bg-slate-900 p-8 text-center text-sm text-slate-400">
			Loading transactions...
		</div>
	{:else if transactions.length === 0}
		<div class="rounded-2xl border border-slate-800 bg-slate-900 p-8 text-center text-xs text-slate-400">
			No transactions recorded yet. Add an account and log your first transaction.
		</div>
	{:else}
		<div class="space-y-2.5">
			{#each transactions as tx (tx.id)}
				<TransactionItem transaction={tx} />
			{/each}
		</div>
	{/if}
</div>
