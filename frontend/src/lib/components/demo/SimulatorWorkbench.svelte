<script lang="ts">
	import { onMount } from 'svelte';
	import {
		fetchSimulatorState,
		settleDemoPending,
		stageDemoManualMatch,
		stageDemoTransaction,
		stageDemoTransfer,
		updateDemoFault,
		type FaultMode,
		type SimFaultState,
		type SimulatorStateResponse
	} from '$lib/api/settings';
	import { demo } from '$lib/stores/demo.svelte';

	interface Props {
		compact?: boolean;
	}

	let { compact = false }: Props = $props();

	type SimTab = 'transactions' | 'settle' | 'transfers' | 'faults' | 'bank';

	let activeTab = $state<SimTab>('transactions');
	let simState = $state<SimulatorStateResponse | null>(null);
	let loading = $state(true);
	let busy = $state(false);
	let errorMsg = $state<string | null>(null);
	let feedbackMsg = $state<string | null>(null);

	// Custom transaction form
	let syncAfterStage = $state(false);
	let txExtAcctId = $state('');
	let txDescription = $state('');
	let txAmount = $state('');
	let txDirection = $state<'outflow' | 'inflow'>('outflow');
	let txDate = $state(new Date().toISOString().slice(0, 10));
	let txPending = $state(false);

	// Custom settle state per transaction id
	let expandedSettleId = $state<string | null>(null);
	let customSettleAmount = $state('');
	let customSettleDesc = $state('');

	// Custom transfer form
	let trFromAcct = $state('');
	let trToAcct = $state('');
	let trAmount = $state('250.00');
	let trDayOffset = $state(0);
	let trDesc = $state('');

	// Fault form
	let faultMode = $state<FaultMode>('NONE');
	let faultPersistent = $state(false);
	let faultRetryAfter = $state(3);
	let faultMessage = $state('');

	let accountMap = $derived(
		new Map((simState?.accounts ?? []).map((a) => [a.external_account_id, a]))
	);
	let pendingTransactions = $derived((simState?.transactions ?? []).filter((t) => t.pending));
	let isSyncing = $derived(demo.syncStatus === 'running' || demo.syncStatus === 'retrying');

	function syncDefaultsFromState(data: SimulatorStateResponse) {
		if (!txExtAcctId && data.accounts.length > 0) {
			const rewards = data.accounts.find((a) => a.external_account_id.includes('rewards'));
			txExtAcctId = (rewards ?? data.accounts[0]).external_account_id;
		}
		if (!trFromAcct && data.accounts.length > 0) {
			trFromAcct = data.accounts[0].external_account_id;
		}
		if (!trToAcct && data.accounts.length > 1) {
			trToAcct = data.accounts[1].external_account_id;
		}
		faultMode = data.fault.mode;
		faultPersistent = data.fault.persistent;
		faultRetryAfter = data.fault.retry_after_seconds || 3;
		faultMessage = data.fault.message || '';
	}

	export async function refresh() {
		loading = true;
		errorMsg = null;
		try {
			const data = await fetchSimulatorState();
			simState = data;
			demo.unsyncedCount = data.unsynced_count;
			syncDefaultsFromState(data);
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to load simulator state';
		} finally {
			loading = false;
		}
	}

	let lastObservedVersion = $state(-1);
	$effect(() => {
		const v = demo.dataVersion;
		if (v !== lastObservedVersion) {
			lastObservedVersion = v;
			if (demo.isDemo && v > 0) {
				void refresh();
			}
		}
	});

	onMount(() => {
		if (demo.isDemo) {
			void refresh();
		} else {
			loading = false;
		}
	});

	async function finishAction(
		res: { workflow_id?: string; sync_error?: string },
		stagedLabel: string
	) {
		feedbackMsg = stagedLabel;
		if (res.sync_error) {
			errorMsg = `Staged, but could not start sync: ${res.sync_error}`;
		}
		if (res.workflow_id) {
			await demo.triggerSyncAndPoll(res.workflow_id);
		}
		await refresh();
	}

	async function handlePresetTransaction(preset: string, label: string) {
		busy = true;
		errorMsg = null;
		feedbackMsg = null;
		try {
			const res = await stageDemoTransaction({
				preset,
				sync_now: syncAfterStage
			});
			await finishAction(
				res,
				syncAfterStage ? `${label} staged & synced.` : `${label} staged in fake bank.`
			);
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to stage preset';
		} finally {
			busy = false;
		}
	}

	async function handleCustomTransaction(e: SubmitEvent, forceSync = false) {
		e.preventDefault();
		const parsed = Number(txAmount);
		if (!txExtAcctId || !txDescription.trim() || !Number.isFinite(parsed) || parsed <= 0) {
			errorMsg = 'Please enter a valid account, merchant description, and positive amount.';
			return;
		}
		const signedAmount = txDirection === 'outflow' ? -Math.abs(parsed) : Math.abs(parsed);
		const shouldSync = forceSync || syncAfterStage;

		busy = true;
		errorMsg = null;
		feedbackMsg = null;
		try {
			const res = await stageDemoTransaction({
				external_account_id: txExtAcctId,
				description: txDescription.trim(),
				amount: signedAmount,
				posted_at: txDate,
				pending: txPending,
				sync_now: shouldSync
			});
			txDescription = '';
			txAmount = '';
			await finishAction(
				res,
				shouldSync ? 'Custom transaction staged & synced.' : 'Custom transaction staged in fake bank.'
			);
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to stage custom transaction';
		} finally {
			busy = false;
		}
	}

	async function handleQuickSettle(
		demoTxId: string,
		opts: { tipPercent?: number; appendSuffix?: string; label: string }
	) {
		busy = true;
		errorMsg = null;
		feedbackMsg = null;
		try {
			const res = await settleDemoPending({
				demo_transaction_id: demoTxId,
				tip_percent: opts.tipPercent,
				append_suffix: opts.appendSuffix,
				sync_now: syncAfterStage
			});
			expandedSettleId = null;
			await finishAction(res, opts.label);
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to settle transaction';
		} finally {
			busy = false;
		}
	}

	function openCustomSettle(tx: { id: string; amount: string; description: string }) {
		if (expandedSettleId === tx.id) {
			expandedSettleId = null;
			return;
		}
		expandedSettleId = tx.id;
		customSettleAmount = Math.abs(Number(tx.amount)).toFixed(2);
		customSettleDesc = tx.description;
	}

	async function handleCustomSettleSubmit(
		demoTxId: string,
		origAmountStr: string,
		forceSync = false
	) {
		const parsed = Number(customSettleAmount);
		if (!Number.isFinite(parsed) || parsed <= 0) {
			errorMsg = 'Settled amount must be greater than zero.';
			return;
		}
		const origSign = Number(origAmountStr) < 0 ? -1 : 1;
		const shouldSync = forceSync || syncAfterStage;

		busy = true;
		errorMsg = null;
		feedbackMsg = null;
		try {
			const res = await settleDemoPending({
				demo_transaction_id: demoTxId,
				settled_amount: origSign * Math.abs(parsed),
				new_description: customSettleDesc.trim(),
				sync_now: shouldSync
			});
			expandedSettleId = null;
			await finishAction(res, 'Pending authorization settled in fake bank.');
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to settle transaction';
		} finally {
			busy = false;
		}
	}

	async function handlePresetTransfer(
		fromExt: string,
		toExt: string,
		amount: number,
		label: string
	) {
		busy = true;
		errorMsg = null;
		feedbackMsg = null;
		try {
			const res = await stageDemoTransfer({
				from_external_account_id: fromExt,
				to_external_account_id: toExt,
				amount,
				day_offset: 0,
				sync_now: syncAfterStage
			});
			await finishAction(res, `${label} staged.`);
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to stage transfer';
		} finally {
			busy = false;
		}
	}

	async function handleCustomTransfer(e: SubmitEvent, forceSync = false) {
		e.preventDefault();
		const parsed = Number(trAmount);
		if (!trFromAcct || !trToAcct || trFromAcct === trToAcct || !Number.isFinite(parsed) || parsed <= 0) {
			errorMsg = 'Select two distinct accounts and a positive transfer amount.';
			return;
		}
		const shouldSync = forceSync || syncAfterStage;
		busy = true;
		errorMsg = null;
		feedbackMsg = null;
		try {
			const res = await stageDemoTransfer({
				from_external_account_id: trFromAcct,
				to_external_account_id: trToAcct,
				amount: parsed,
				day_offset: Number(trDayOffset) || 0,
				description: trDesc.trim(),
				sync_now: shouldSync
			});
			trDesc = '';
			await finishAction(res, 'Cross-account transfer staged.');
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to stage transfer';
		} finally {
			busy = false;
		}
	}

	async function handleStageManualMatch(manualId: string, tipPercent: number) {
		busy = true;
		errorMsg = null;
		feedbackMsg = null;
		try {
			const res = await stageDemoManualMatch({
				manual_transaction_id: manualId,
				tip_percent: tipPercent,
				day_offset: 0,
				sync_now: syncAfterStage
			});
			await finishAction(
				res,
				tipPercent > 0
					? `Staged bank feed match (+${tipPercent}% tip) for manual entry.`
					: 'Staged exact bank feed match for manual entry.'
			);
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to stage manual match';
		} finally {
			busy = false;
		}
	}

	async function handleSaveFault(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		errorMsg = null;
		feedbackMsg = null;
		try {
			const payload: SimFaultState = {
				mode: faultMode,
				persistent: faultPersistent,
				retry_after_seconds: Number(faultRetryAfter) || 3,
				message: faultMessage.trim()
			};
			await updateDemoFault(payload);
			feedbackMsg =
				faultMode === 'NONE'
					? 'Bank faults cleared (normal operation).'
					: `Fault mode ${faultMode} armed for ${faultPersistent ? 'all future syncs' : 'next sync'}.`;
			await refresh();
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to update fault config';
		} finally {
			busy = false;
		}
	}

	async function handleTriggerSync() {
		errorMsg = null;
		feedbackMsg = null;
		await demo.triggerSyncAndPoll();
		await refresh();
	}

	function fmtAmount(raw: string): string {
		const n = Number(raw);
		if (!Number.isFinite(n)) return raw;
		const abs = Math.abs(n).toFixed(2);
		return n < 0 ? `-$${abs}` : `+$${abs}`;
	}
</script>

<div class="space-y-4">
	<!-- Top Simulator Status Strip -->
	<div class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-slate-200 dark:border-slate-800 bg-slate-50 dark:bg-slate-950/60 p-3">
		<div class="flex flex-wrap items-center gap-2">
			<span
				data-testid="sim-unsynced-badge"
				class="inline-flex items-center px-2 py-0.5 rounded-full text-[11px] font-semibold {(simState?.unsynced_count ?? 0) > 0
					? 'bg-amber-100 text-amber-800 dark:bg-amber-500/20 dark:text-amber-300'
					: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-500/20 dark:text-emerald-300'}"
			>
				{(simState?.unsynced_count ?? 0) > 0
					? `${simState?.unsynced_count} staged (unsynced)`
					: 'Bank in sync'}
			</span>

			{#if simState && simState.fault.mode !== 'NONE'}
				<span
					data-testid="sim-active-fault-pill"
					class="inline-flex items-center px-2 py-0.5 rounded-full text-[11px] font-semibold bg-rose-100 text-rose-800 dark:bg-rose-500/20 dark:text-rose-300"
				>
					Fault: {simState.fault.mode}
				</span>
			{/if}
		</div>

		<div class="flex items-center gap-2">
			<label class="inline-flex items-center gap-1.5 text-[11px] text-slate-600 dark:text-slate-300 cursor-pointer select-none">
				<input
					type="checkbox"
					bind:checked={syncAfterStage}
					class="rounded border-slate-300 dark:border-slate-600 text-emerald-600 focus:ring-emerald-500"
				/>
				<span>Auto-sync on stage</span>
			</label>
			<button
				type="button"
				onclick={handleTriggerSync}
				disabled={isSyncing || busy}
				data-testid="sim-sync-now-btn"
				class="px-3 py-1.5 rounded-lg text-xs font-semibold bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 text-white transition-colors"
			>
				{isSyncing ? 'Syncing...' : 'Sync Now'}
			</button>
		</div>
	</div>

	<!-- Live Temporal Sync Status Banner -->
	{#if demo.syncStatus !== 'idle'}
		<div
			data-testid="sim-sync-status-banner"
			class="rounded-lg border px-3 py-2 text-xs {demo.syncStatus === 'failed'
				? 'border-rose-200 bg-rose-50 text-rose-800 dark:border-rose-500/30 dark:bg-rose-500/10 dark:text-rose-300'
				: demo.syncStatus === 'retrying'
					? 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-300'
					: demo.syncStatus === 'completed'
						? 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-300'
						: 'border-sky-200 bg-sky-50 text-sky-800 dark:border-sky-500/30 dark:bg-sky-500/10 dark:text-sky-300'}"
		>
			<p class="font-medium">{demo.syncMessage}</p>
			{#if demo.syncStatus === 'completed' && demo.lastSyncResult?.warnings && demo.lastSyncResult.warnings.length > 0}
				<ul class="mt-1 list-disc list-inside text-[11px] text-amber-700 dark:text-amber-300">
					{#each demo.lastSyncResult.warnings as w, idx (idx)}
						<li>{w}</li>
					{/each}
				</ul>
			{/if}
		</div>
	{/if}

	{#if errorMsg}
		<div class="rounded-lg border border-rose-200 dark:border-rose-500/30 bg-rose-50 dark:bg-rose-500/10 px-3 py-2 text-xs text-rose-700 dark:text-rose-300">
			{errorMsg}
		</div>
	{/if}

	{#if feedbackMsg}
		<div class="rounded-lg border border-sky-200 dark:border-sky-500/30 bg-sky-50 dark:bg-sky-500/10 px-3 py-2 text-xs text-sky-800 dark:text-sky-300">
			{feedbackMsg}
		</div>
	{/if}

	<!-- Sub-tab Navigation -->
	<div
		role="tablist"
		aria-label="SimpleFIN simulator tools"
		class="flex flex-wrap gap-1 border-b border-slate-200 dark:border-slate-800 pb-2"
	>
		{#each [
			{ id: 'transactions', label: 'Transactions' },
			{ id: 'settle', label: `Settle Pending (${pendingTransactions.length})` },
			{ id: 'transfers', label: 'Transfers & Manual' },
			{ id: 'faults', label: 'Faults' },
			{ id: 'bank', label: 'Fake Bank' }
		] as tab (tab.id)}
			<button
				type="button"
				role="tab"
				aria-selected={activeTab === tab.id}
				data-testid={`sim-tab-${tab.id}`}
				onclick={() => (activeTab = tab.id as SimTab)}
				class="px-2.5 py-1.5 rounded-lg text-xs font-medium transition-colors {activeTab === tab.id
					? 'bg-slate-900 text-white dark:bg-slate-100 dark:text-slate-900'
					: 'text-slate-600 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800'}"
			>
				{tab.label}
			</button>
		{/each}
	</div>

	{#if loading && !simState}
		<p class="py-6 text-center text-xs text-slate-500 dark:text-slate-400">
			Loading fake SimpleFIN bridge state...
		</p>
	{:else if simState}
		<!-- Tab 1: Transactions -->
		{#if activeTab === 'transactions'}
			<div class="space-y-4">
				<div>
					<h4 class="text-xs font-semibold text-slate-700 dark:text-slate-300 mb-2">
						Quick Realistic Presets
					</h4>
					<div class="grid {compact ? 'grid-cols-1 sm:grid-cols-2' : 'grid-cols-2 sm:grid-cols-3'} gap-2">
						<button
							type="button"
							disabled={busy}
							data-testid="preset-coffee-btn"
							onclick={() => handlePresetTransaction('coffee', 'Coffee (-$6.50)')}
							class="text-left p-2.5 rounded-lg border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 hover:border-emerald-500/60 transition-colors"
						>
							<div class="text-xs font-semibold text-slate-900 dark:text-slate-100">Coffee Shop</div>
							<div class="text-[11px] font-mono text-rose-600 dark:text-rose-400">-$6.50 · Posted</div>
						</button>
						<button
							type="button"
							disabled={busy}
							data-testid="preset-groceries-btn"
							onclick={() => handlePresetTransaction('groceries', 'Groceries (-$84.30)')}
							class="text-left p-2.5 rounded-lg border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 hover:border-emerald-500/60 transition-colors"
						>
							<div class="text-xs font-semibold text-slate-900 dark:text-slate-100">Whole Foods</div>
							<div class="text-[11px] font-mono text-rose-600 dark:text-rose-400">-$84.30 · Posted</div>
						</button>
						<button
							type="button"
							disabled={busy}
							data-testid="preset-dining-pending-btn"
							onclick={() => handlePresetTransaction('dining_pending', 'Dining Auth (-$42.00)')}
							class="text-left p-2.5 rounded-lg border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 hover:border-emerald-500/60 transition-colors"
						>
							<div class="text-xs font-semibold text-slate-900 dark:text-slate-100">Bistro Auth</div>
							<div class="text-[11px] font-mono text-amber-600 dark:text-amber-400">-$42.00 · Pending</div>
						</button>
						<button
							type="button"
							disabled={busy}
							data-testid="preset-paycheck-btn"
							onclick={() => handlePresetTransaction('paycheck', 'Paycheck (+$2,600.00)')}
							class="text-left p-2.5 rounded-lg border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 hover:border-emerald-500/60 transition-colors"
						>
							<div class="text-xs font-semibold text-slate-900 dark:text-slate-100">Paycheck</div>
							<div class="text-[11px] font-mono text-emerald-600 dark:text-emerald-400">+$2,600.00 · Posted</div>
						</button>
						<button
							type="button"
							disabled={busy}
							data-testid="preset-utility-btn"
							onclick={() => handlePresetTransaction('utility', 'Utility Bill (-$115.00)')}
							class="text-left p-2.5 rounded-lg border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 hover:border-emerald-500/60 transition-colors"
						>
							<div class="text-xs font-semibold text-slate-900 dark:text-slate-100">Internet Bill</div>
							<div class="text-[11px] font-mono text-rose-600 dark:text-rose-400">-$115.00 · Posted</div>
						</button>
					</div>
				</div>

				<form
					onsubmit={(e) => handleCustomTransaction(e, false)}
					class="space-y-3 rounded-xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 p-3.5"
				>
					<h4 class="text-xs font-semibold text-slate-800 dark:text-slate-200">
						Custom Simulated Transaction
					</h4>
					<div class="grid grid-cols-1 sm:grid-cols-2 gap-2.5">
						<div>
							<label for="sim-tx-acct" class="block text-[11px] text-slate-500 dark:text-slate-400 mb-1">
								Fake Bank Account
							</label>
							<select
								id="sim-tx-acct"
								bind:value={txExtAcctId}
								class="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-2.5 py-1.5 text-xs text-slate-900 dark:text-slate-100"
							>
								{#each simState.accounts as acct (acct.external_account_id)}
									<option value={acct.external_account_id}>{acct.name}</option>
								{/each}
							</select>
						</div>
						<div>
							<label for="sim-tx-dir" class="block text-[11px] text-slate-500 dark:text-slate-400 mb-1">
								Direction
							</label>
							<select
								id="sim-tx-dir"
								bind:value={txDirection}
								class="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-2.5 py-1.5 text-xs text-slate-900 dark:text-slate-100"
							>
								<option value="outflow">Expense (Outflow)</option>
								<option value="inflow">Deposit / Refund (Inflow)</option>
							</select>
						</div>
					</div>

					<div>
						<label for="sim-tx-desc" class="block text-[11px] text-slate-500 dark:text-slate-400 mb-1">
							Raw Bank Merchant Description
						</label>
						<input
							id="sim-tx-desc"
							type="text"
							bind:value={txDescription}
							placeholder="e.g. POS DEBIT TRADER JOES #104"
							class="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-2.5 py-1.5 text-xs text-slate-900 dark:text-slate-100"
							required
						/>
					</div>

					<div class="grid grid-cols-2 gap-2.5">
						<div>
							<label for="sim-tx-amt" class="block text-[11px] text-slate-500 dark:text-slate-400 mb-1">
								Amount ($)
							</label>
							<input
								id="sim-tx-amt"
								type="number"
								step="0.01"
								min="0.01"
								bind:value={txAmount}
								placeholder="25.00"
								class="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-2.5 py-1.5 text-xs font-mono text-slate-900 dark:text-slate-100"
								required
							/>
						</div>
						<div>
							<label for="sim-tx-date" class="block text-[11px] text-slate-500 dark:text-slate-400 mb-1">
								Date
							</label>
							<input
								id="sim-tx-date"
								type="date"
								bind:value={txDate}
								class="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-2.5 py-1.5 text-xs text-slate-900 dark:text-slate-100"
							/>
						</div>
					</div>

					<label class="inline-flex items-center gap-2 text-xs text-slate-700 dark:text-slate-300 cursor-pointer">
						<input
							type="checkbox"
							bind:checked={txPending}
							class="rounded border-slate-300 dark:border-slate-600 text-emerald-600"
						/>
						<span>Stage as Pending authorization</span>
					</label>

					<div class="flex flex-wrap justify-end gap-2 pt-1">
						<button
							type="submit"
							disabled={busy}
							class="px-3 py-1.5 rounded-lg text-xs font-medium border border-slate-300 dark:border-slate-700 bg-slate-50 hover:bg-slate-100 dark:bg-slate-800 dark:hover:bg-slate-700 text-slate-800 dark:text-slate-100"
						>
							Stage in Bank
						</button>
						<button
							type="button"
							disabled={busy || isSyncing}
							onclick={(e) => void handleCustomTransaction(e as unknown as SubmitEvent, true)}
							class="px-3 py-1.5 rounded-lg text-xs font-semibold bg-emerald-600 hover:bg-emerald-500 text-white"
						>
							Stage &amp; Sync
						</button>
					</div>
				</form>
			</div>

		<!-- Tab 2: Settle Pending -->
		{:else if activeTab === 'settle'}
			<div class="space-y-3">
				<p class="text-xs text-slate-500 dark:text-slate-400">
					Promote a pending authorization to posted with an optional tip delta and/or branch suffix to test reconciliation (`MatchPostedToPending`).
				</p>

				{#if pendingTransactions.length === 0}
					<div class="rounded-xl border border-dashed border-slate-300 dark:border-slate-700 py-8 text-center text-xs text-slate-500 dark:text-slate-400">
						No pending transactions in the fake SimpleFIN bank. Stage a pending auth from the Transactions tab first.
					</div>
				{:else}
					<div class="space-y-2.5">
						{#each pendingTransactions as ptx (ptx.id)}
							{@const acct = accountMap.get(ptx.external_account_id)}
							<div
								data-testid="sim-pending-card"
								class="rounded-xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 p-3 space-y-2.5"
							>
								<div class="flex items-start justify-between gap-2">
									<div class="min-w-0">
										<div class="flex items-center gap-1.5 flex-wrap">
											<span class="text-xs font-semibold text-slate-900 dark:text-slate-100 truncate">
												{ptx.description}
											</span>
											<span class="text-[10px] px-1.5 py-0.5 rounded bg-amber-100 text-amber-800 dark:bg-amber-500/20 dark:text-amber-300">
												Pending
											</span>
											<span class="text-[10px] px-1.5 py-0.5 rounded {ptx.is_synced ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-300' : 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-400'}">
												{ptx.is_synced ? 'Synced' : 'Staged'}
											</span>
										</div>
										<p class="text-[11px] text-slate-500 dark:text-slate-400 mt-0.5">
											{acct?.name ?? ptx.external_account_id} · {ptx.posted_at.slice(0, 10)}
										</p>
									</div>
									<span class="text-xs font-mono font-semibold text-slate-900 dark:text-slate-100 shrink-0">
										{fmtAmount(ptx.amount)}
									</span>
								</div>

								<div class="flex flex-wrap gap-1.5">
									<button
										type="button"
										disabled={busy}
										data-testid={`settle-exact-${ptx.id}`}
										onclick={() => handleQuickSettle(ptx.id, { label: `Settled ${ptx.description} (exact).` })}
										class="px-2.5 py-1 rounded-md text-[11px] font-medium bg-slate-100 hover:bg-slate-200 dark:bg-slate-800 dark:hover:bg-slate-700 text-slate-800 dark:text-slate-200"
									>
										Settle Exact
									</button>
									<button
										type="button"
										disabled={busy}
										onclick={() =>
											handleQuickSettle(ptx.id, {
												tipPercent: 20,
												label: `Settled ${ptx.description} with +20% tip.`
											})}
										class="px-2.5 py-1 rounded-md text-[11px] font-medium bg-emerald-50 hover:bg-emerald-100 dark:bg-emerald-500/15 dark:hover:bg-emerald-500/25 text-emerald-700 dark:text-emerald-300"
									>
										+20% Tip
									</button>
									<button
										type="button"
										disabled={busy}
										onclick={() =>
											handleQuickSettle(ptx.id, {
												tipPercent: 20,
												appendSuffix: 'DOWNTOWN',
												label: `Settled ${ptx.description} (+20% tip + DOWNTOWN suffix).`
											})}
										class="px-2.5 py-1 rounded-md text-[11px] font-medium bg-sky-50 hover:bg-sky-100 dark:bg-sky-500/15 dark:hover:bg-sky-500/25 text-sky-700 dark:text-sky-300"
									>
										+20% Tip + Suffix
									</button>
									<button
										type="button"
										onclick={() => openCustomSettle(ptx)}
										class="px-2.5 py-1 rounded-md text-[11px] text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200"
									>
										Custom...
									</button>
								</div>

								{#if expandedSettleId === ptx.id}
									<div class="pt-2 border-t border-slate-200 dark:border-slate-800 space-y-2">
										<div class="grid grid-cols-2 gap-2">
											<div>
												<label for={`settle-amt-${ptx.id}`} class="block text-[11px] text-slate-500 dark:text-slate-400 mb-0.5">
													Settled Amount ($)
												</label>
												<input
													id={`settle-amt-${ptx.id}`}
													type="number"
													step="0.01"
													bind:value={customSettleAmount}
													class="w-full rounded border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-2 py-1 text-xs font-mono text-slate-900 dark:text-slate-100"
												/>
											</div>
											<div>
												<label for={`settle-desc-${ptx.id}`} class="block text-[11px] text-slate-500 dark:text-slate-400 mb-0.5">
													Settled Merchant
												</label>
												<input
													id={`settle-desc-${ptx.id}`}
													type="text"
													bind:value={customSettleDesc}
													class="w-full rounded border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-2 py-1 text-xs text-slate-900 dark:text-slate-100"
												/>
											</div>
										</div>
										<div class="flex justify-end gap-2">
											<button
												type="button"
												disabled={busy}
												onclick={() => handleCustomSettleSubmit(ptx.id, ptx.amount, false)}
												class="px-2.5 py-1 rounded text-[11px] font-medium border border-slate-300 dark:border-slate-700 text-slate-700 dark:text-slate-200"
											>
												Stage Settle
											</button>
											<button
												type="button"
												disabled={busy || isSyncing}
												onclick={() => handleCustomSettleSubmit(ptx.id, ptx.amount, true)}
												class="px-2.5 py-1 rounded text-[11px] font-semibold bg-emerald-600 text-white"
											>
												Settle &amp; Sync
											</button>
										</div>
									</div>
								{/if}
							</div>
						{/each}
					</div>
				{/if}
			</div>

		<!-- Tab 3: Transfers & Manual Match -->
		{:else if activeTab === 'transfers'}
			<div class="space-y-4">
				<div class="space-y-2">
					<h4 class="text-xs font-semibold text-slate-700 dark:text-slate-300">
						Cross-Account Transfer Presets
					</h4>
					<div class="grid grid-cols-1 sm:grid-cols-2 gap-2">
						<button
							type="button"
							disabled={busy}
							onclick={() =>
								handlePresetTransfer(
									'demo-checking-001',
									'demo-savings-001',
									500,
									'Checking -> Savings ($500) transfer'
								)}
							class="text-left p-2.5 rounded-lg border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 hover:border-emerald-500/60"
						>
							<div class="text-xs font-semibold text-slate-900 dark:text-slate-100">
								Checking → Savings
							</div>
							<div class="text-[11px] font-mono text-slate-500 dark:text-slate-400">$500.00 offsetting legs</div>
						</button>
						<button
							type="button"
							disabled={busy}
							onclick={() =>
								handlePresetTransfer(
									'demo-checking-001',
									'demo-cc-rewards-001',
									350,
									'Checking -> Everyday Rewards ($350) payment'
								)}
							class="text-left p-2.5 rounded-lg border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 hover:border-emerald-500/60"
						>
							<div class="text-xs font-semibold text-slate-900 dark:text-slate-100">
								Pay Everyday Rewards Card
							</div>
							<div class="text-[11px] font-mono text-slate-500 dark:text-slate-400">$350.00 card payment</div>
						</button>
					</div>
				</div>

				<form
					onsubmit={(e) => handleCustomTransfer(e, false)}
					class="rounded-xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 p-3.5 space-y-2.5"
				>
					<h4 class="text-xs font-semibold text-slate-800 dark:text-slate-200">
						Custom Transfer Pair
					</h4>
					<div class="grid grid-cols-1 sm:grid-cols-2 gap-2">
						<div>
							<label for="sim-tr-from" class="block text-[11px] text-slate-500 dark:text-slate-400 mb-1">
								From Account
							</label>
							<select
								id="sim-tr-from"
								bind:value={trFromAcct}
								class="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-2 py-1.5 text-xs text-slate-900 dark:text-slate-100"
							>
								{#each simState.accounts as acct (acct.external_account_id)}
									<option value={acct.external_account_id}>{acct.name}</option>
								{/each}
							</select>
						</div>
						<div>
							<label for="sim-tr-to" class="block text-[11px] text-slate-500 dark:text-slate-400 mb-1">
								To Account
							</label>
							<select
								id="sim-tr-to"
								bind:value={trToAcct}
								class="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-2 py-1.5 text-xs text-slate-900 dark:text-slate-100"
							>
								{#each simState.accounts as acct (acct.external_account_id)}
									<option value={acct.external_account_id}>{acct.name}</option>
								{/each}
							</select>
						</div>
					</div>
					<div class="grid grid-cols-2 gap-2">
						<div>
							<label for="sim-tr-amt" class="block text-[11px] text-slate-500 dark:text-slate-400 mb-1">
								Amount ($)
							</label>
							<input
								id="sim-tr-amt"
								type="number"
								step="0.01"
								min="0.01"
								bind:value={trAmount}
								class="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-2 py-1.5 text-xs font-mono text-slate-900 dark:text-slate-100"
							/>
						</div>
						<div>
							<label for="sim-tr-offset" class="block text-[11px] text-slate-500 dark:text-slate-400 mb-1">
								Posting Lag (0–3 days)
							</label>
							<input
								id="sim-tr-offset"
								type="number"
								min="0"
								max="3"
								bind:value={trDayOffset}
								class="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-2 py-1.5 text-xs font-mono text-slate-900 dark:text-slate-100"
							/>
						</div>
					</div>
					<div class="flex justify-end gap-2 pt-1">
						<button
							type="submit"
							disabled={busy}
							class="px-3 py-1.5 rounded-lg text-xs font-medium border border-slate-300 dark:border-slate-700 text-slate-700 dark:text-slate-200"
						>
							Stage Transfer
						</button>
						<button
							type="button"
							disabled={busy || isSyncing}
							onclick={(e) => void handleCustomTransfer(e as unknown as SubmitEvent, true)}
							class="px-3 py-1.5 rounded-lg text-xs font-semibold bg-emerald-600 text-white"
						>
							Stage &amp; Sync
						</button>
					</div>
				</form>

				<!-- Manual-to-Bank Deduplication Test -->
				<div class="space-y-2">
					<h4 class="text-xs font-semibold text-slate-700 dark:text-slate-300">
						Manual-to-Bank Match Test (`MANUAL_OFFLINE`)
					</h4>
					{#if simState.manual_transactions.length === 0}
						<p class="text-xs text-slate-500 dark:text-slate-400 rounded-lg border border-dashed border-slate-300 dark:border-slate-700 p-3">
							No manual transactions found in the demo workspace yet. Add a manual expense in the Budget → Transactions panel to test bank deduplication.
						</p>
					{:else}
						<div class="space-y-2">
							{#each simState.manual_transactions as mtx (mtx.id)}
								<div class="rounded-lg border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 p-2.5 flex flex-wrap items-center justify-between gap-2">
									<div class="min-w-0">
										<p class="text-xs font-semibold text-slate-900 dark:text-slate-100 truncate">
											{mtx.merchant_raw}
										</p>
										<p class="text-[11px] text-slate-500 dark:text-slate-400">
											{mtx.account_name} · {mtx.transaction_date} · <span class="font-mono">{fmtAmount(mtx.amount)}</span>
										</p>
									</div>
									{#if mtx.external_account_id}
										<div class="flex items-center gap-1.5">
											<button
												type="button"
												disabled={busy}
												onclick={() => handleStageManualMatch(mtx.id, 0)}
												class="px-2 py-1 rounded text-[11px] font-medium bg-slate-100 hover:bg-slate-200 dark:bg-slate-800 dark:hover:bg-slate-700 text-slate-800 dark:text-slate-200"
											>
												Exact Match
											</button>
											<button
												type="button"
												disabled={busy}
												onclick={() => handleStageManualMatch(mtx.id, 10)}
												class="px-2 py-1 rounded text-[11px] font-medium bg-emerald-50 hover:bg-emerald-100 dark:bg-emerald-500/15 dark:hover:bg-emerald-500/25 text-emerald-700 dark:text-emerald-300"
											>
												+10% Tip Match
											</button>
										</div>
									{:else}
										<span class="text-[11px] text-slate-400">Unlinked account</span>
									{/if}
								</div>
							{/each}
						</div>
					{/if}
				</div>
			</div>

		<!-- Tab 4: Faults -->
		{:else if activeTab === 'faults'}
			<form
				onsubmit={handleSaveFault}
				class="rounded-xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 p-3.5 space-y-3"
			>
				<p class="text-xs text-slate-500 dark:text-slate-400">
					Inject SimpleFIN bridge errors to observe Temporal retries, partial account warnings, and non-retryable auth failures.
				</p>

				<fieldset class="space-y-2">
					<legend class="text-xs font-semibold text-slate-800 dark:text-slate-200 mb-1">
						Fault Mode
					</legend>
					{#each [
						{ value: 'NONE', label: 'None — Normal SimpleFIN Operation' },
						{ value: 'RATE_LIMIT_429', label: 'HTTP 429 — Rate Limited (tests Temporal backoff & retries)' },
						{ value: 'PARTIAL_WARNING', label: 'Partial Account Warning — returns DataResponse.Errors' },
						{ value: 'AUTH_ERROR_403', label: 'HTTP 403 — Revoked / Expired Access URL (non-retryable)' }
					] as opt (opt.value)}
						<label class="flex items-start gap-2 text-xs text-slate-700 dark:text-slate-300 cursor-pointer">
							<input
								type="radio"
								name="fault-mode"
								value={opt.value}
								bind:group={faultMode}
								class="mt-0.5 text-emerald-600"
							/>
							<span>{opt.label}</span>
						</label>
					{/each}
				</fieldset>

				<label class="flex items-center gap-2 text-xs text-slate-700 dark:text-slate-300 cursor-pointer">
					<input
						type="checkbox"
						bind:checked={faultPersistent}
						class="rounded border-slate-300 dark:border-slate-600 text-emerald-600"
					/>
					<span>Persist fault across syncs (if unchecked, auto-clears after 1 request)</span>
				</label>

				{#if faultMode === 'RATE_LIMIT_429'}
					<div>
						<label for="sim-fault-retry" class="block text-[11px] text-slate-500 dark:text-slate-400 mb-1">
							Retry-After Header (seconds)
						</label>
						<input
							id="sim-fault-retry"
							type="number"
							min="1"
							max="60"
							bind:value={faultRetryAfter}
							class="w-32 rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-2.5 py-1.5 text-xs font-mono text-slate-900 dark:text-slate-100"
						/>
					</div>
				{/if}

				{#if faultMode !== 'NONE'}
					<div>
						<label for="sim-fault-msg" class="block text-[11px] text-slate-500 dark:text-slate-400 mb-1">
							Custom Message (optional)
						</label>
						<input
							id="sim-fault-msg"
							type="text"
							bind:value={faultMessage}
							placeholder="Leave blank for default SimpleFIN message"
							class="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-2.5 py-1.5 text-xs text-slate-900 dark:text-slate-100"
						/>
					</div>
				{/if}

				<div class="flex justify-end">
					<button
						type="submit"
						disabled={busy}
						data-testid="save-fault-btn"
						class="px-3.5 py-1.5 rounded-lg text-xs font-semibold bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 text-white"
					>
						Save Fault Config
					</button>
				</div>
			</form>

		<!-- Tab 5: Fake Bank Inspector -->
		{:else if activeTab === 'bank'}
			<div class="space-y-4">
				<div>
					<h4 class="text-xs font-semibold text-slate-700 dark:text-slate-300 mb-2">
						Fake SimpleFIN Accounts ({simState.accounts.length})
					</h4>
					<div class="grid grid-cols-1 sm:grid-cols-2 gap-2">
						{#each simState.accounts as acct (acct.external_account_id)}
							<div class="rounded-lg border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 p-2.5 flex items-center justify-between gap-2">
								<div class="min-w-0">
									<p class="text-xs font-semibold text-slate-900 dark:text-slate-100 truncate">
										{acct.name}
									</p>
									<p class="text-[10px] font-mono text-slate-500 dark:text-slate-400">
										{acct.external_account_id} · {acct.account_type}
									</p>
								</div>
								<span class="text-xs font-mono font-semibold text-slate-900 dark:text-slate-100">
									${Number(acct.balance).toFixed(2)}
								</span>
							</div>
						{/each}
					</div>
				</div>

				<div>
					<h4 class="text-xs font-semibold text-slate-700 dark:text-slate-300 mb-2">
						Fake SimpleFIN Transaction Feed ({simState.transactions.length})
					</h4>
					<div class="divide-y divide-slate-100 dark:divide-slate-800 rounded-xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 max-h-72 overflow-y-auto">
						{#each simState.transactions as stx (stx.id)}
							<div class="px-3 py-2 flex items-center justify-between gap-2">
								<div class="min-w-0">
									<div class="flex items-center gap-1.5 flex-wrap">
										<span class="text-xs font-medium text-slate-900 dark:text-slate-100 truncate">
											{stx.description}
										</span>
										{#if stx.pending}
											<span class="text-[10px] px-1.5 py-0.5 rounded bg-amber-100 text-amber-800 dark:bg-amber-500/20 dark:text-amber-300">
												Pending
											</span>
										{/if}
										<span class="text-[10px] px-1.5 py-0.5 rounded {stx.is_synced ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-300' : 'bg-sky-100 text-sky-700 dark:bg-sky-500/20 dark:text-sky-300'}">
											{stx.is_synced ? 'Synced' : 'Staged'}
										</span>
									</div>
									<p class="text-[10px] text-slate-500 dark:text-slate-400">
										{accountMap.get(stx.external_account_id)?.name ?? stx.external_account_id} · {stx.posted_at.slice(0, 10)}
									</p>
								</div>
								<span class="text-xs font-mono font-semibold {Number(stx.amount) >= 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-slate-800 dark:text-slate-200'}">
									{fmtAmount(stx.amount)}
								</span>
							</div>
						{/each}
					</div>
				</div>
			</div>
		{/if}
	{/if}
</div>
