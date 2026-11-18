<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/stores/auth.svelte';
	import { fetchAccounts, fetchSolvencyReport } from '$lib/api/transactions';
	import { triggerBankSyncNow } from '$lib/api/simplefin';
	import SimpleFINSetupModal from '$lib/components/accounts/SimpleFINSetupModal.svelte';
	import type { AccountItem, SolvencyReport } from '$lib/types/transaction';

	let accounts = $state<AccountItem[]>([]);
	let solvency = $state<SolvencyReport | null>(null);
	let loading = $state(true);
	let syncing = $state(false);
	let modalOpen = $state(false);
	let statusMessage = $state<string | null>(null);

	async function loadAccounts() {
		await auth.ready();
		if (!auth.isAuthenticated) {
			void goto('/login');
			return;
		}
		loading = true;
		try {
			const [accts, solv] = await Promise.all([
				fetchAccounts(),
				fetchSolvencyReport().catch(() => null)
			]);
			accounts = accts;
			solvency = solv;
		} catch (err) {
			statusMessage = err instanceof Error ? err.message : 'Failed to load accounts';
		} finally {
			loading = false;
		}
	}

	async function handleSyncNow() {
		syncing = true;
		statusMessage = null;
		try {
			await triggerBankSyncNow();
			statusMessage = 'Bank sync workflow triggered.';
		} catch (err) {
			statusMessage = err instanceof Error ? err.message : 'Failed to trigger bank sync';
		} finally {
			syncing = false;
		}
	}

	onMount(() => {
		void loadAccounts();
	});
</script>

<div class="max-w-5xl mx-auto p-4 sm:p-6 space-y-6">
	<div class="flex flex-wrap items-center justify-between gap-4">
		<div>
			<h2 class="text-xl font-bold text-slate-100">Accounts</h2>
			<p class="text-xs text-slate-400">On-budget cash, credit card modes, and SimpleFIN bank connections</p>
		</div>

		<div class="flex items-center gap-2">
			<button
				type="button"
				onclick={handleSyncNow}
				disabled={syncing}
				class="text-xs px-3 py-1.5 rounded-md bg-slate-800 hover:bg-slate-700 text-slate-200 border border-slate-700 transition-colors"
			>
				{syncing ? 'Syncing...' : 'Sync Now'}
			</button>
			<button
				type="button"
				onclick={() => (modalOpen = true)}
				class="text-xs px-3 py-1.5 rounded-md bg-emerald-600 hover:bg-emerald-500 text-white font-medium transition-colors"
			>
				Connect SimpleFIN
			</button>
		</div>
	</div>

	{#if statusMessage}
		<div class="bg-slate-900 border border-slate-700 text-slate-200 text-xs px-3 py-2 rounded-md">
			{statusMessage}
		</div>
	{/if}

	{#if solvency}
		<div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
			<div class="bg-slate-900 border border-slate-800 rounded-lg p-3">
				<p class="text-xs text-slate-400">Total Liquid Cash</p>
				<p class="text-lg font-mono font-semibold text-emerald-400">${solvency.totalLiquidCash.toFixed(2)}</p>
			</div>
			<div class="bg-slate-900 border border-slate-800 rounded-lg p-3">
				<p class="text-xs text-slate-400">Pay-In-Full Card Debt</p>
				<p class="text-lg font-mono font-semibold text-slate-200">${solvency.totalCreditCardDebt.toFixed(2)}</p>
			</div>
			<div class="bg-slate-900 border border-slate-800 rounded-lg p-3">
				<p class="text-xs text-slate-400">Solvency Buffer</p>
				<p class="text-lg font-mono font-semibold {solvency.isFloatingDebt ? 'text-rose-400' : 'text-emerald-400'}">
					${solvency.solvencyBuffer.toFixed(2)}
				</p>
			</div>
		</div>
	{/if}

	{#if loading}
		<div class="text-sm text-slate-400 py-8 text-center">Loading accounts...</div>
	{:else if accounts.length === 0}
		<div class="text-sm text-slate-400 py-10 text-center border border-dashed border-slate-800 rounded-lg">
			No accounts linked yet. Click "Connect SimpleFIN" to link your bank accounts.
		</div>
	{:else}
		<div class="grid grid-cols-1 md:grid-cols-2 gap-3">
			{#each accounts as acct (acct.id)}
				<div class="bg-slate-900 border border-slate-800 rounded-lg p-4 flex items-center justify-between">
					<div>
						<div class="flex items-center gap-2">
							<h3 class="text-sm font-semibold text-slate-100">{acct.name}</h3>
							<span class="text-[11px] uppercase px-1.5 py-0.5 rounded bg-slate-800 text-slate-300">
								{acct.type.replace('_', ' ')}
							</span>
						</div>
						<p class="text-xs text-slate-400 mt-1">
							{acct.isOnBudget ? 'On Budget' : 'Off Budget'}
							{#if acct.type === 'credit_card'}
								• {acct.cardMode === 'DEBT_PAYDOWN' ? 'Debt Paydown' : 'Pay In Full'}
							{/if}
						</p>
					</div>
					<div class="text-right">
						<span class="text-base font-mono font-semibold {acct.currentBalance < 0 ? 'text-rose-400' : 'text-emerald-400'}">
							{acct.currentBalance < 0 ? '-' : ''}${Math.abs(acct.currentBalance).toFixed(2)}
						</span>
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>

<SimpleFINSetupModal
	open={modalOpen}
	onClose={() => (modalOpen = false)}
	onLinked={() => void loadAccounts()}
/>
