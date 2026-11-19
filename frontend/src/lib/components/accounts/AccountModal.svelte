<script lang="ts">
	import { onMount } from 'svelte';
	import { createAccount, deleteAccount, updateAccount } from '$lib/api/transactions';
	import type {
		AccountItem,
		AccountType,
		CreateAccountPayload,
		UpdateAccountPayload
	} from '$lib/types/transaction';

	interface CategoryOption {
		id: string;
		name: string;
		groupName: string;
	}

	interface Props {
		open: boolean;
		editingAccount: AccountItem | null;
		categories: CategoryOption[];
		onClose: () => void;
		onSaved: (message?: string) => void;
	}

	let { open, editingAccount, categories, onClose, onSaved }: Props = $props();

	let dialogEl = $state<HTMLDialogElement | null>(null);

	let name = $state('');
	let type = $state<AccountType>('checking');
	let isOnBudget = $state(true);
	let isOnBudgetDirty = $state(false);
	let balanceInput = $state('0.00');
	let isCreditOverpaid = $state(false);
	let recordBalanceAdjustment = $state(false);

	let cardMode = $state<'PAY_IN_FULL' | 'DEBT_PAYDOWN'>('PAY_IN_FULL');
	let aprInput = $state('');
	let statementDueDayInput = $state('');
	let debtPayoffCategoryId = $state('');
	let initialDebtBaselineInput = $state('');

	let isSubmitting = $state(false);
	let errorMsg = $state<string | null>(null);
	let confirmDelete = $state(false);
	let confirmUnlink = $state(false);

	let isEditing = $derived(editingAccount !== null);
	let isLinked = $derived(Boolean(editingAccount?.externalAccountId));
	let isDebtAccount = $derived(type === 'credit_card' || type === 'loan');

	function round2(n: number): number {
		return Math.round((n + Number.EPSILON) * 100) / 100;
	}

	function defaultOnBudgetForType(t: AccountType): boolean {
		return t !== 'investment' && t !== 'loan';
	}

	let computedBalance = $derived.by(() => {
		const raw = Number(balanceInput);
		const safe = Number.isFinite(raw) ? raw : 0;
		if (isDebtAccount) {
			const mag = Math.abs(safe);
			if (mag === 0) return 0;
			return round2(isCreditOverpaid ? mag : -mag);
		}
		return round2(safe);
	});

	let balanceDelta = $derived.by(() => {
		if (!editingAccount) return 0;
		return round2(computedBalance - editingAccount.currentBalance);
	});

	let hasBalanceChanged = $derived(
		isEditing && !isLinked && Math.abs(balanceDelta) >= 0.005
	);

	function fmtSigned(n: number): string {
		const abs = Math.abs(n).toLocaleString('en-US', {
			minimumFractionDigits: 2,
			maximumFractionDigits: 2
		});
		return n < 0 ? `-$${abs}` : `+$${abs}`;
	}

	function fmtMoney(n: number): string {
		const abs = Math.abs(n).toLocaleString('en-US', {
			minimumFractionDigits: 2,
			maximumFractionDigits: 2
		});
		return n < 0 ? `-$${abs}` : `$${abs}`;
	}

	function hydrateForm(acct: AccountItem | null) {
		errorMsg = null;
		confirmDelete = false;
		confirmUnlink = false;
		recordBalanceAdjustment = false;
		isOnBudgetDirty = false;

		if (acct) {
			name = acct.name;
			type = acct.type;
			isOnBudget = acct.isOnBudget;
			if (acct.type === 'credit_card' || acct.type === 'loan') {
				isCreditOverpaid = acct.currentBalance > 0;
				balanceInput = Math.abs(acct.currentBalance).toFixed(2);
			} else {
				isCreditOverpaid = false;
				balanceInput = acct.currentBalance.toFixed(2);
			}
			cardMode = acct.cardMode ?? 'PAY_IN_FULL';
			aprInput = acct.aprPercentage > 0 ? acct.aprPercentage.toString() : '';
			statementDueDayInput = acct.statementDueDay != null ? String(acct.statementDueDay) : '';
			debtPayoffCategoryId = acct.debtPayoffCategoryId ?? '';
			initialDebtBaselineInput =
				acct.initialDebtBaseline > 0 ? acct.initialDebtBaseline.toFixed(2) : '';
		} else {
			name = '';
			type = 'checking';
			isOnBudget = true;
			balanceInput = '0.00';
			isCreditOverpaid = false;
			cardMode = 'PAY_IN_FULL';
			aprInput = '';
			statementDueDayInput = '';
			debtPayoffCategoryId = '';
			initialDebtBaselineInput = '';
		}
	}

	$effect(() => {
		if (!dialogEl) return;
		if (open) {
			hydrateForm(editingAccount);
			if (!dialogEl.open) {
				dialogEl.showModal();
			}
		} else if (dialogEl.open) {
			dialogEl.close();
		}
	});

	// Fallback light-dismiss for browsers lacking native <dialog closedby="any">
	onMount(() => {
		if (!dialogEl || 'closedBy' in HTMLDialogElement.prototype) return;
		const el = dialogEl;
		let pressedOutside = false;
		const isOutside = (e: PointerEvent) => {
			const r = el.getBoundingClientRect();
			return e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom;
		};
		const onDown = (e: PointerEvent) => {
			pressedOutside = el.open && isOutside(e);
		};
		const onUp = (e: PointerEvent) => {
			if (pressedOutside && el.open && isOutside(e)) {
				el.close();
			}
			pressedOutside = false;
		};
		el.addEventListener('pointerdown', onDown);
		el.addEventListener('pointerup', onUp);
		return () => {
			el.removeEventListener('pointerdown', onDown);
			el.removeEventListener('pointerup', onUp);
		};
	});

	function handleNativeClose() {
		if (dialogEl) {
			dialogEl.returnValue = '';
		}
		confirmDelete = false;
		confirmUnlink = false;
		onClose();
	}

	function handleTypeChange() {
		if (!isEditing && !isOnBudgetDirty) {
			isOnBudget = defaultOnBudgetForType(type);
		}
		if (type !== 'credit_card' && type !== 'loan') {
			isCreditOverpaid = false;
		}
	}

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		const trimmedName = name.trim();
		if (!trimmedName) {
			errorMsg = 'Account name is required.';
			return;
		}

		const parsedApr = aprInput.trim() === '' ? 0 : Number(aprInput);
		if (!Number.isFinite(parsedApr) || parsedApr < 0) {
			errorMsg = 'APR must be 0 or greater.';
			return;
		}

		let parsedDueDay: number | null = null;
		if (type === 'credit_card' && statementDueDayInput.trim() !== '') {
			const d = Number(statementDueDayInput);
			if (!Number.isInteger(d) || d < 1 || d > 31) {
				errorMsg = 'Statement due day must be a whole number between 1 and 31.';
				return;
			}
			parsedDueDay = d;
		}

		if (type === 'credit_card' && cardMode === 'DEBT_PAYDOWN' && !debtPayoffCategoryId) {
			errorMsg = 'Please select a Debt Payoff Category for Debt Paydown mode.';
			return;
		}

		const parsedBaseline =
			initialDebtBaselineInput.trim() !== ''
				? Math.max(0, round2(Number(initialDebtBaselineInput) || 0))
				: !isEditing
					? Math.abs(computedBalance)
					: 0;

		isSubmitting = true;
		errorMsg = null;

		try {
			if (!editingAccount) {
				const payload: CreateAccountPayload = {
					name: trimmedName,
					type,
					currentBalance: computedBalance,
					isOnBudget,
					cardMode: type === 'credit_card' ? cardMode : 'PAY_IN_FULL',
					aprPercentage: type === 'credit_card' || type === 'loan' ? round2(parsedApr) : 0,
					statementDueDay: type === 'credit_card' ? parsedDueDay : null,
					debtPayoffCategoryId:
						type === 'credit_card' && cardMode === 'DEBT_PAYDOWN' ? debtPayoffCategoryId : null,
					initialDebtBaseline:
						type === 'credit_card' && cardMode === 'DEBT_PAYDOWN' ? parsedBaseline : 0
				};
				await createAccount(payload);
				onSaved(`Created account "${trimmedName}".`);
			} else {
				const payload: UpdateAccountPayload = {
					name: trimmedName,
					type,
					isOnBudget,
					aprPercentage: type === 'credit_card' || type === 'loan' ? round2(parsedApr) : 0
				};

				if (!isLinked) {
					payload.currentBalance = computedBalance;
					if (hasBalanceChanged && recordBalanceAdjustment) {
						payload.recordBalanceAdjustment = true;
					}
				}

				if (type === 'credit_card') {
					payload.cardMode = cardMode;
					if (parsedDueDay !== null) {
						payload.statementDueDay = parsedDueDay;
					} else if (editingAccount.statementDueDay !== null) {
						payload.clearStatementDueDay = true;
					}

					if (cardMode === 'DEBT_PAYDOWN') {
						payload.debtPayoffCategoryId = debtPayoffCategoryId;
						payload.initialDebtBaseline = parsedBaseline;
					} else {
						if (editingAccount.debtPayoffCategoryId !== null) {
							payload.clearDebtPayoffCategory = true;
						}
						payload.initialDebtBaseline = 0;
					}
				} else {
					payload.cardMode = 'PAY_IN_FULL';
					if (editingAccount.statementDueDay !== null) {
						payload.clearStatementDueDay = true;
					}
					if (editingAccount.debtPayoffCategoryId !== null) {
						payload.clearDebtPayoffCategory = true;
					}
					payload.initialDebtBaseline = 0;
				}

				await updateAccount(editingAccount.id, payload);
				onSaved(`Updated account "${trimmedName}".`);
			}
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to save account.';
		} finally {
			isSubmitting = false;
		}
	}

	async function handleDeleteConfirmed() {
		if (!editingAccount) return;
		isSubmitting = true;
		errorMsg = null;
		try {
			const deletedName = editingAccount.name;
			await deleteAccount(editingAccount.id);
			onSaved(`Deleted account "${deletedName}".`);
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to delete account.';
		} finally {
			isSubmitting = false;
		}
	}

	async function handleUnlinkConfirmed() {
		if (!editingAccount) return;
		isSubmitting = true;
		errorMsg = null;
		try {
			const acctName = editingAccount.name;
			await updateAccount(editingAccount.id, { unlinkExternal: true });
			onSaved(`Unlinked "${acctName}" from SimpleFIN. It is now manually tracked.`);
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to unlink account.';
		} finally {
			isSubmitting = false;
		}
	}
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<dialog
	bind:this={dialogEl}
	aria-labelledby="account-modal-title"
	closedby="any"
	onclose={handleNativeClose}
	class="m-auto w-full max-w-lg rounded-2xl border border-slate-800 bg-slate-900 p-0 text-slate-100 shadow-2xl max-h-[90dvh] overflow-y-auto"
>
	<div class="p-6">
		<div class="flex items-center justify-between border-b border-slate-800 pb-3">
			<div class="flex items-center gap-2">
				<h2 id="account-modal-title" class="text-base font-semibold text-white">
					{isEditing ? 'Edit Account' : 'Add Manual Account'}
				</h2>
				{#if isEditing}
					<span
						class="text-[11px] font-medium px-2 py-0.5 rounded-full border {isLinked
							? 'bg-sky-500/15 text-sky-300 border-sky-500/30'
							: 'bg-slate-800 text-slate-300 border-slate-700'}"
					>
						{isLinked ? 'SimpleFIN' : 'Manual'}
					</span>
				{/if}
			</div>
			<button
				type="button"
				aria-label="Close account modal"
				onclick={() => dialogEl?.close()}
				class="rounded-lg p-1 text-slate-400 hover:bg-slate-800 hover:text-white"
			>
				✕
			</button>
		</div>

		<form onsubmit={handleSubmit} class="mt-4 space-y-4">
			<div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
				<div>
					<label for="acct-name" class="block text-xs font-medium text-slate-300 mb-1">
						Account Name
					</label>
					<!-- svelte-ignore a11y_autofocus -->
					<input
						id="acct-name"
						type="text"
						bind:value={name}
						placeholder="e.g. Wallet Cash"
						autofocus
						required
						class="w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white focus:border-emerald-500 focus:outline-none"
					/>
				</div>

				<div>
					<label for="acct-type" class="block text-xs font-medium text-slate-300 mb-1">
						Account Type
					</label>
					<select
						id="acct-type"
						bind:value={type}
						onchange={handleTypeChange}
						class="w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white focus:border-emerald-500 focus:outline-none"
					>
						<option value="checking">Checking</option>
						<option value="savings">Savings</option>
						<option value="cash">Cash</option>
						<option value="credit_card">Credit Card</option>
						<option value="investment">Investment</option>
						<option value="loan">Loan</option>
					</select>
				</div>
			</div>

			<label
				for="acct-on-budget"
				class="flex items-start gap-2.5 rounded-lg border border-slate-800 bg-slate-950/60 p-3 cursor-pointer"
			>
				<input
					id="acct-on-budget"
					type="checkbox"
					bind:checked={isOnBudget}
					onchange={() => (isOnBudgetDirty = true)}
					class="mt-0.5 rounded border-slate-600 bg-slate-900 text-emerald-500 focus:ring-emerald-500"
				/>
				<div>
					<span class="block text-xs font-medium text-slate-200">On-Budget Account</span>
					<span class="block text-[11px] text-slate-400">
						Include in monthly budget spending &amp; solvency buffer. Uncheck for off-budget tracking.
					</span>
				</div>
			</label>

			{#if isLinked && editingAccount}
				<div class="rounded-lg border border-slate-800 bg-slate-950 p-3.5 space-y-2.5">
					<div class="flex items-center justify-between">
						<div>
							<p class="text-xs font-medium text-slate-300">Current Balance</p>
							<p class="text-[11px] text-slate-400">Synced automatically via SimpleFIN</p>
						</div>
						<span
							class="text-base font-mono font-semibold {editingAccount.currentBalance < 0
								? 'text-rose-400'
								: 'text-emerald-400'}"
						>
							{fmtMoney(editingAccount.currentBalance)}
						</span>
					</div>

					{#if !confirmUnlink}
						<div class="pt-1 border-t border-slate-800/80 flex justify-end">
							<button
								type="button"
								onclick={() => (confirmUnlink = true)}
								disabled={isSubmitting}
								class="text-xs text-amber-300 hover:text-amber-200 underline"
							>
								Unlink from SimpleFIN
							</button>
						</div>
					{:else}
						<div class="pt-2 border-t border-slate-800/80 space-y-2">
							<p class="text-xs text-amber-200">
								Convert to a manual account? SimpleFIN will stop syncing this account's balance and transactions.
							</p>
							<div class="flex justify-end gap-2">
								<button
									type="button"
									onclick={() => (confirmUnlink = false)}
									disabled={isSubmitting}
									class="rounded-md border border-slate-700 px-2.5 py-1 text-xs text-slate-300 hover:bg-slate-800"
								>
									Cancel
								</button>
								<button
									type="button"
									onclick={handleUnlinkConfirmed}
									disabled={isSubmitting}
									class="rounded-md bg-amber-600 px-3 py-1 text-xs font-semibold text-white hover:bg-amber-500 disabled:opacity-50"
								>
									Confirm Unlink
								</button>
							</div>
						</div>
					{/if}
				</div>
			{:else}
				<div class="space-y-2.5">
					<div>
						<label for="acct-balance" class="block text-xs font-medium text-slate-300 mb-1">
							{#if isDebtAccount}
								{isCreditOverpaid ? 'Credit Balance ($)' : 'Amount Owed ($)'}
							{:else}
								{isEditing ? 'Current Balance ($)' : 'Starting Balance ($)'}
							{/if}
						</label>
						<input
							id="acct-balance"
							type="number"
							step="0.01"
							min={isDebtAccount ? '0' : undefined}
							bind:value={balanceInput}
							required
							class="w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm font-mono text-white focus:border-emerald-500 focus:outline-none"
						/>
						{#if isDebtAccount}
							<div class="mt-2 flex items-center justify-between gap-2">
								<label for="acct-credit-overpaid" class="flex items-center gap-2 text-xs text-slate-400 cursor-pointer">
									<input
										id="acct-credit-overpaid"
										type="checkbox"
										bind:checked={isCreditOverpaid}
										class="rounded border-slate-600 bg-slate-900 text-emerald-500"
									/>
									<span>Account has a positive / credit balance (overpaid)</span>
								</label>
								<span class="text-[11px] font-mono text-slate-400">
									Stored: {fmtMoney(computedBalance)}
								</span>
							</div>
						{/if}
					</div>

					{#if hasBalanceChanged}
						<label
							for="acct-record-adj"
							class="flex items-start gap-2.5 rounded-lg border border-emerald-500/30 bg-emerald-500/5 p-3 cursor-pointer"
						>
							<input
								id="acct-record-adj"
								type="checkbox"
								bind:checked={recordBalanceAdjustment}
								class="mt-0.5 rounded border-slate-600 bg-slate-900 text-emerald-500"
							/>
							<div>
								<span class="block text-xs font-medium text-emerald-300">
									Record balance adjustment transaction ({fmtSigned(balanceDelta)})
								</span>
								<span class="block text-[11px] text-slate-400">
									Adds an uncategorized "Balance Adjustment" entry to the transaction ledger.
								</span>
							</div>
						</label>
					{/if}
				</div>
			{/if}

			{#if type === 'credit_card'}
				<div class="rounded-lg border border-slate-800 bg-slate-950/60 p-3.5 space-y-3">
					<p class="text-xs font-semibold uppercase tracking-wider text-slate-400">
						Credit Card Settings
					</p>

					<div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
						<div>
							<label for="acct-card-mode" class="block text-xs font-medium text-slate-300 mb-1">
								Card Mode
							</label>
							<select
								id="acct-card-mode"
								bind:value={cardMode}
								class="w-full rounded-lg border border-slate-700 bg-slate-950 px-2.5 py-2 text-xs text-white focus:border-emerald-500 focus:outline-none"
							>
								<option value="PAY_IN_FULL">Pay in Full</option>
								<option value="DEBT_PAYDOWN">Debt Paydown</option>
							</select>
						</div>

						<div>
							<label for="acct-apr" class="block text-xs font-medium text-slate-300 mb-1">
								APR (%)
							</label>
							<input
								id="acct-apr"
								type="number"
								step="0.01"
								min="0"
								bind:value={aprInput}
								placeholder="0.00"
								class="w-full rounded-lg border border-slate-700 bg-slate-950 px-2.5 py-2 text-xs text-white focus:border-emerald-500 focus:outline-none"
							/>
						</div>

						<div>
							<label for="acct-due-day" class="block text-xs font-medium text-slate-300 mb-1">
								Due Day (1–31)
							</label>
							<input
								id="acct-due-day"
								type="number"
								step="1"
								min="1"
								max="31"
								bind:value={statementDueDayInput}
								placeholder="Optional"
								class="w-full rounded-lg border border-slate-700 bg-slate-950 px-2.5 py-2 text-xs text-white focus:border-emerald-500 focus:outline-none"
							/>
						</div>
					</div>

					{#if cardMode === 'DEBT_PAYDOWN'}
						<div class="grid grid-cols-1 sm:grid-cols-2 gap-3 pt-1">
							<div>
								<label for="acct-debt-cat" class="block text-xs font-medium text-slate-300 mb-1">
									Debt Payoff Category
								</label>
								<select
									id="acct-debt-cat"
									bind:value={debtPayoffCategoryId}
									required
									class="w-full rounded-lg border border-slate-700 bg-slate-950 px-2.5 py-2 text-xs text-white focus:border-emerald-500 focus:outline-none"
								>
									<option value="">Select category...</option>
									{#each categories as cat (cat.id)}
										<option value={cat.id}>{cat.groupName} — {cat.name}</option>
									{/each}
								</select>
								{#if categories.length === 0}
									<p class="text-[11px] text-amber-300 mt-1">
										No categories found. Create one on the Budget page first.
									</p>
								{/if}
							</div>

							<div>
								<label for="acct-debt-baseline" class="block text-xs font-medium text-slate-300 mb-1">
									Initial Debt Baseline ($)
								</label>
								<input
									id="acct-debt-baseline"
									type="number"
									step="0.01"
									min="0"
									bind:value={initialDebtBaselineInput}
									placeholder="Defaults to owed balance"
									class="w-full rounded-lg border border-slate-700 bg-slate-950 px-2.5 py-2 text-xs text-white focus:border-emerald-500 focus:outline-none"
								/>
							</div>
						</div>
					{/if}
				</div>
			{:else if type === 'loan'}
				<div>
					<label for="acct-apr" class="block text-xs font-medium text-slate-300 mb-1">
						APR (%)
					</label>
					<input
						id="acct-apr"
						type="number"
						step="0.01"
						min="0"
						bind:value={aprInput}
						placeholder="Optional (e.g. 5.99)"
						class="w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white focus:border-emerald-500 focus:outline-none"
					/>
				</div>
			{/if}

			{#if errorMsg}
				<p class="text-xs text-rose-400 bg-rose-500/10 border border-rose-500/30 rounded-lg px-3 py-2">
					{errorMsg}
				</p>
			{/if}

			{#if isEditing && confirmDelete}
				<div class="rounded-lg border border-rose-500/40 bg-rose-500/10 p-3 space-y-2">
					<p class="text-xs text-rose-200">
						Delete "{editingAccount?.name}" and all of its transactions? This cannot be undone.
					</p>
					<div class="flex items-center justify-end gap-2">
						<button
							type="button"
							onclick={handleDeleteConfirmed}
							disabled={isSubmitting}
							class="rounded-lg bg-rose-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-rose-500 disabled:opacity-50"
						>
							Confirm Delete
						</button>
						<button
							type="button"
							onclick={() => (confirmDelete = false)}
							disabled={isSubmitting}
							class="rounded-lg border border-slate-700 px-3 py-1.5 text-xs font-medium text-slate-200 hover:bg-slate-800"
						>
							Keep Account
						</button>
					</div>
				</div>
			{/if}

			<div class="flex items-center justify-between pt-2">
				{#if isEditing && !confirmDelete}
					<button
						type="button"
						onclick={() => (confirmDelete = true)}
						disabled={isSubmitting}
						class="rounded-lg border border-rose-500/40 px-3 py-2 text-xs font-medium text-rose-400 hover:bg-rose-500/10 disabled:opacity-50"
					>
						Delete
					</button>
				{:else}
					<div></div>
				{/if}

				<div class="flex items-center gap-2">
					<button
						type="button"
						onclick={() => dialogEl?.close()}
						disabled={isSubmitting}
						class="rounded-lg border border-slate-700 px-3.5 py-2 text-xs font-medium text-slate-300 hover:bg-slate-800"
					>
						Cancel
					</button>
					<button
						type="submit"
						disabled={isSubmitting}
						class="rounded-lg bg-emerald-600 px-4 py-2 text-xs font-semibold text-white hover:bg-emerald-500 disabled:opacity-50"
					>
						{isSubmitting ? 'Saving...' : isEditing ? 'Save Changes' : 'Create Account'}
					</button>
				</div>
			</div>
		</form>
	</div>
</dialog>

<style>
	dialog:modal {
		overscroll-behavior: contain;
	}
	dialog::backdrop {
		overflow: hidden;
		overscroll-behavior: contain;
		background: rgb(0 0 0 / 0.65);
		backdrop-filter: blur(4px);
	}
</style>
