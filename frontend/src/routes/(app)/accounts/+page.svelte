<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/stores/auth.svelte';
	import { demo } from '$lib/stores/demo.svelte';
	import { fetchCategories } from '$lib/api/budget';
	import { fetchAccounts, fetchSolvencyReport } from '$lib/api/transactions';
	import AccountModal from '$lib/components/accounts/AccountModal.svelte';
	import SimpleFINSetupModal from '$lib/components/accounts/SimpleFINSetupModal.svelte';
	import type { AccountItem, SolvencyReport } from '$lib/types/transaction';

	let accounts = $state<AccountItem[]>([]);
	let categories = $state<Array<{ id: string; name: string; groupName: string }>>([]);
	let solvency = $state<SolvencyReport | null>(null);
	let loading = $state(true);
	let syncing = $state(false);
	let simplefinModalOpen = $state(false);
	let simplefinAutoDiscover = $state(false);
	let accountModalOpen = $state(false);
	let editingAccount = $state<AccountItem | null>(null);
	let statusMessage = $state<string | null>(null);

	let cashAndCreditAccounts = $derived(
		accounts.filter((a) => a.type !== 'investment' && a.type !== 'loan')
	);
	let trackingAccounts = $derived(
		accounts.filter((a) => a.type === 'investment' || a.type === 'loan')
	);
	let cashAndCreditTotal = $derived(
		cashAndCreditAccounts.reduce((sum, a) => sum + a.currentBalance, 0)
	);
	let trackingTotal = $derived(trackingAccounts.reduce((sum, a) => sum + a.currentBalance, 0));
	let reservationByAccountId = $derived(
		new Map((solvency?.accountReservations ?? []).map((r) => [r.accountId, r]))
	);
	let linkedSimpleFINIds = $derived(
		accounts.map((a) => a.externalAccountId).filter((id): id is string => Boolean(id))
	);

	function fmtMoney(n: number): string {
		const abs = Math.abs(n).toLocaleString('en-US', {
			minimumFractionDigits: 2,
			maximumFractionDigits: 2
		});
		return n < 0 ? `-$${abs}` : `$${abs}`;
	}

	function openAddAccount() {
		editingAccount = null;
		accountModalOpen = true;
	}

	function openEditAccount(acct: AccountItem) {
		editingAccount = acct;
		accountModalOpen = true;
	}

	function handleCardKeydown(e: KeyboardEvent, acct: AccountItem) {
		if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			openEditAccount(acct);
		}
	}

	async function loadAccounts() {
		await auth.ready();
		if (!auth.isAuthenticated && !auth.canAccessOffline) {
			void goto('/login');
			return;
		}
		loading = true;
		try {
			const [accts, solv, cats] = await Promise.all([
				fetchAccounts(),
				fetchSolvencyReport().catch(() => null),
				fetchCategories().catch(() => [])
			]);
			accounts = accts;
			solvency = solv;
			categories = cats;
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
			const res = await demo.triggerSyncAndPoll();
			if (res?.status === 'FAILED') {
				statusMessage = res.last_error || 'Bank sync workflow failed.';
			} else if (demo.syncMessage) {
				statusMessage = demo.syncMessage;
			} else {
				statusMessage = 'Bank sync completed.';
			}
			await loadAccounts();
		} catch (err) {
			statusMessage = err instanceof Error ? err.message : 'Failed to trigger bank sync';
		} finally {
			syncing = false;
		}
	}

	let lastDataVersion = $state(-1);
	$effect(() => {
		const v = demo.dataVersion;
		if (v !== lastDataVersion) {
			lastDataVersion = v;
			if (v > 0) {
				void loadAccounts();
			}
		}
	});

	onMount(() => {
		void loadAccounts();
	});
</script>

{#snippet accountCard(acct: AccountItem)}
	{@const reservation = reservationByAccountId.get(acct.id)}
	{@const unreservedBalance = reservation ? acct.currentBalance - reservation.reservedTotal : null}
	<div
		role="button"
		tabindex="0"
		onclick={() => openEditAccount(acct)}
		onkeydown={(e) => handleCardKeydown(e, acct)}
		class="group cursor-pointer bg-surface border border-border hover:border-accent-pop/50 rounded-xl p-4 space-y-2.5 shadow-xs transition-colors"
	>
		<div class="flex items-center justify-between gap-3">
			<div class="min-w-0">
				<div class="flex flex-wrap items-center gap-2">
					<h3 class="text-sm font-semibold text-ink-primary group-hover:text-accent-pop transition-colors truncate">
						{acct.name}
					</h3>
					<span class="text-[11px] uppercase px-1.5 py-0.5 rounded bg-subtle text-ink-secondary">
						{acct.type.replace('_', ' ')}
					</span>
					<span
						class="text-[10px] font-medium px-1.5 py-0.5 rounded border {acct.externalAccountId
							? 'bg-accent-tint text-accent-pop border-accent-pop/30'
							: 'bg-subtle text-ink-secondary border-border'}"
					>
						{acct.externalAccountId ? 'SimpleFIN' : 'Manual'}
					</span>
					<span class="text-[11px] text-ink-muted opacity-0 group-hover:opacity-100 transition-opacity">
						Edit
					</span>
				</div>
				<p class="text-xs text-ink-secondary mt-1">
					{#if acct.type === 'investment' || acct.type === 'loan'}
						Balance Tracked
					{:else if acct.type === 'credit_card'}
						{acct.cardMode === 'DEBT_PAYDOWN' ? 'Debt Paydown' : 'Pay In Full'}
					{:else}
						In Monthly Budget
					{/if}
					{#if (acct.type === 'credit_card' || acct.type === 'loan') && acct.aprPercentage > 0}
						• {acct.aprPercentage.toFixed(2)}% APR
					{/if}
				</p>
			</div>
			<div class="text-right shrink-0">
				<span class="text-base font-mono font-semibold {acct.currentBalance < 0 ? 'text-status-danger' : 'text-status-good'}">
					{fmtMoney(acct.currentBalance)}
				</span>
			</div>
		</div>

		{#if reservation && reservation.funds.length > 0}
			<div
				data-testid="account-fund-reservations"
				class="pt-2.5 border-t border-border/80 space-y-1.5 text-xs"
			>
				<div class="flex flex-wrap items-center justify-between gap-2">
					<span class="text-ink-secondary">
						Reserved in Funds:
						<strong class="font-mono text-ink-primary">{fmtMoney(reservation.reservedTotal)}</strong>
					</span>
					<span
						class="font-mono font-semibold {unreservedBalance !== null && unreservedBalance < 0
							? 'text-status-danger'
							: 'text-status-good'}"
					>
						Unreserved: {fmtMoney(unreservedBalance ?? 0)}
					</span>
				</div>
				{#if unreservedBalance !== null && unreservedBalance < -0.005}
					<p class="text-[11px] font-medium text-status-danger bg-status-danger-bg rounded-md px-2 py-1">
						Over-reserved by {fmtMoney(Math.abs(unreservedBalance))} — reserved funds exceed this account's balance.
					</p>
				{/if}
				<div class="flex flex-wrap gap-1.5 pt-0.5">
					{#each reservation.funds as fund (fund.categoryId)}
						<span class="inline-flex items-center gap-1 rounded-full bg-subtle border border-border px-2 py-0.5 text-[11px] text-ink-secondary">
							<span>{fund.name}:</span>
							<span class="font-mono font-medium text-ink-primary">{fmtMoney(fund.balance)}</span>
						</span>
					{/each}
				</div>
			</div>
		{/if}
	</div>
{/snippet}

<div class="max-w-5xl mx-auto p-4 sm:p-6 space-y-6">
	<div class="flex flex-wrap items-center justify-between gap-4">
		<div>
			<h2 class="text-xl font-bold text-ink-primary">Accounts</h2>
			<p class="text-xs text-ink-secondary">
				Cash &amp; credit accounts, reserved sinking funds, investment/loan tracking, and SimpleFIN links
			</p>
		</div>

		<div class="flex flex-wrap items-center gap-2">
			<button
				type="button"
				onclick={handleSyncNow}
				disabled={syncing || demo.syncStatus === 'running' || demo.syncStatus === 'retrying'}
				class="text-xs px-3 py-1.5 rounded-lg bg-surface hover:bg-surface-hover text-ink-primary border border-border disabled:opacity-50 transition-colors"
			>
				{syncing || demo.syncStatus === 'running' || demo.syncStatus === 'retrying' ? 'Syncing...' : 'Sync Now'}
			</button>
			<button
				type="button"
				onclick={() => {
					simplefinAutoDiscover = false;
					simplefinModalOpen = true;
				}}
				class="text-xs px-3 py-1.5 rounded-lg bg-surface hover:bg-surface-hover text-ink-primary border border-border transition-colors"
			>
				{demo.simplefin.connected && !demo.isDemo ? 'Link SimpleFIN Accounts' : 'Connect SimpleFIN'}
			</button>
			<button
				type="button"
				onclick={openAddAccount}
				class="text-xs px-3 py-1.5 rounded-lg bg-accent-primary hover:opacity-90 text-accent-primary-text font-semibold transition-opacity"
			>
				+ Add Account
			</button>
		</div>
	</div>

	{#if statusMessage}
		<div class="bg-surface border border-border text-ink-primary text-xs px-3 py-2 rounded-lg">
			{statusMessage}
		</div>
	{/if}

	{#if solvency}
		<div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
			<div class="bg-surface border border-border rounded-xl p-3.5 shadow-xs">
				<p class="text-xs text-ink-secondary">Total Liquid Cash</p>
				<p class="text-lg font-mono font-semibold text-status-good">{fmtMoney(solvency.totalLiquidCash)}</p>
			</div>
			<div class="bg-surface border border-border rounded-xl p-3.5 shadow-xs">
				<p class="text-xs text-ink-secondary">Pay-In-Full Card Debt</p>
				<p class="text-lg font-mono font-semibold text-ink-primary">{fmtMoney(solvency.totalCreditCardDebt)}</p>
			</div>
			<div class="bg-surface border border-border rounded-xl p-3.5 shadow-xs">
				<p class="text-xs text-ink-secondary">Solvency Buffer</p>
				<p class="text-lg font-mono font-semibold {solvency.isFloatingDebt ? 'text-status-danger' : 'text-status-good'}">
					{fmtMoney(solvency.solvencyBuffer)}
				</p>
			</div>
		</div>
	{/if}

	{#if loading}
		<div class="text-sm text-ink-secondary py-8 text-center">Loading accounts...</div>
	{:else if accounts.length === 0}
		<div class="text-sm text-ink-secondary py-10 text-center border border-dashed border-border rounded-xl bg-surface/50">
			No accounts yet. Click "+ Add Account" to track an account manually or "Connect SimpleFIN" to link your bank.
		</div>
	{:else}
		<div class="space-y-6">
			{#if cashAndCreditAccounts.length > 0}
				<section class="space-y-2.5">
					<div class="flex items-center justify-between px-1">
						<h3 class="text-xs font-semibold uppercase tracking-wider text-ink-secondary">
							Cash &amp; Credit Accounts ({cashAndCreditAccounts.length})
						</h3>
						<span class="text-xs font-mono font-medium {cashAndCreditTotal < 0 ? 'text-status-danger' : 'text-status-good'}">
							Net: {fmtMoney(cashAndCreditTotal)}
						</span>
					</div>
					<div class="grid grid-cols-1 md:grid-cols-2 gap-3">
						{#each cashAndCreditAccounts as acct (acct.id)}
							{@render accountCard(acct)}
						{/each}
					</div>
				</section>
			{/if}

			{#if trackingAccounts.length > 0}
				<section class="space-y-2.5">
					<div class="flex items-center justify-between px-1">
						<h3 class="text-xs font-semibold uppercase tracking-wider text-ink-secondary">
							Investments &amp; Loans ({trackingAccounts.length})
						</h3>
						<span class="text-xs font-mono font-medium {trackingTotal < 0 ? 'text-status-danger' : 'text-ink-primary'}">
							Net: {fmtMoney(trackingTotal)}
						</span>
					</div>
					<div class="grid grid-cols-1 md:grid-cols-2 gap-3">
						{#each trackingAccounts as acct (acct.id)}
							{@render accountCard(acct)}
						{/each}
					</div>
				</section>
			{/if}
		</div>
	{/if}
</div>

<AccountModal
	open={accountModalOpen}
	{editingAccount}
	{categories}
	simplefinConnected={demo.simplefin.connected && !demo.isDemo}
	isDemoMode={demo.isDemo}
	onOpenSimpleFIN={() => {
		accountModalOpen = false;
		editingAccount = null;
		simplefinAutoDiscover = demo.simplefin.connected && !demo.isDemo;
		simplefinModalOpen = true;
	}}
	onClose={() => {
		accountModalOpen = false;
		editingAccount = null;
	}}
	onSaved={(msg) => {
		accountModalOpen = false;
		editingAccount = null;
		if (msg) statusMessage = msg;
		void loadAccounts();
	}}
/>

<SimpleFINSetupModal
	open={simplefinModalOpen}
	linkedExternalIds={linkedSimpleFINIds}
	autoDiscover={simplefinAutoDiscover}
	onClose={() => {
		simplefinModalOpen = false;
		simplefinAutoDiscover = false;
	}}
	onLinked={(count) => {
		if (count && count > 0) {
			statusMessage = `Linked ${count} ${count === 1 ? 'account' : 'accounts'} via SimpleFIN — running initial bank sync...`;
		}
		void demo.load().catch(() => {});
		void loadAccounts();
	}}
/>
