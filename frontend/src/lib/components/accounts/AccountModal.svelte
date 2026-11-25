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
		simplefinConnected?: boolean;
		isDemoMode?: boolean;
		onOpenSimpleFIN?: () => void;
		onClose: () => void;
		onSaved: (message?: string) => void;
	}

	let {
		open,
		editingAccount,
		categories,
		simplefinConnected = false,
		isDemoMode = false,
		onOpenSimpleFIN,
		onClose,
		onSaved
	}: Props = $props();

	let dialogEl = $state<HTMLDialogElement | null>(null);

	let name = $state('');
	let type = $state<AccountType>('checking');
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
	let isTrackingType = $derived(type === 'investment' || type === 'loan');

	function round2(n: number): number {
		return Math.round((n + Number.EPSILON) * 100) / 100;
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

		if (acct) {
			name = acct.name;
			type = acct.type;
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
					isOnBudget: !isTrackingType,
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
					isOnBudget: !isTrackingType,
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
	class="m-auto w-full max-w-lg rounded-2xl border border-border bg-surface p-0 text-ink-primary shadow-2xl max-h-[90dvh] overflow-y-auto"
>
	<div class="p-6">
		<div class="flex items-center justify-between border-b border-border pb-3">
			<div class="flex items-center gap-2">
				<h2 id="account-modal-title" class="text-base font-semibold text-ink-primary">
					{isEditing ? 'Edit Account' : 'Add Manual Account'}
				</h2>
				{#if isEditing}
					<span
						class="text-[11px] font-medium px-2 py-0.5 rounded-full border {isLinked
							? 'bg-accent-tint text-accent-pop border-accent-pop/30'
							: 'bg-subtle text-ink-secondary border-border'}"
					>
						{isLinked ? 'SimpleFIN' : 'Manual'}
					</span>
				{/if}
			</div>
			<button
				type="button"
				aria-label="Close account modal"
				onclick={() => dialogEl?.close()}
				class="rounded-lg p-1 text-ink-muted hover:bg-surface-hover hover:text-ink-primary transition-colors"
			>
				✕
			</button>
		</div>

		{#if !isEditing && onOpenSimpleFIN && !isDemoMode}
			<div
				data-testid="account-modal-simplefin-callout"
				class="mt-4 rounded-xl border border-accent-pop/30 bg-accent-tint/50 px-3.5 py-2.5 flex flex-wrap items-center justify-between gap-2.5"
			>
				<div class="min-w-0 flex-1">
					<p class="text-xs font-semibold text-ink-primary">
						{simplefinConnected ? 'Adding a bank account from SimpleFIN?' : 'Want automatic bank sync?'}
					</p>
					<p class="text-[11px] text-ink-secondary">
						{simplefinConnected
							? 'Fetch newly linked accounts from your existing SimpleFIN connection.'
							: 'Connect SimpleFIN Bridge to import accounts automatically.'}
					</p>
				</div>
				<button
					type="button"
					data-testid="switch-to-simplefin-btn"
					onclick={() => {
						dialogEl?.close();
						onOpenSimpleFIN();
					}}
					class="shrink-0 rounded-lg bg-accent-primary px-3 py-1.5 text-xs font-semibold text-accent-primary-text hover:opacity-90 transition-opacity"
				>
					{simplefinConnected ? 'Fetch from SimpleFIN' : 'Connect SimpleFIN'}
				</button>
			</div>
		{/if}

		<form onsubmit={handleSubmit} class="mt-4 space-y-4">
			<div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
				<div>
					<label for="acct-name" class="block text-xs font-medium text-ink-secondary mb-1">
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
						class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
					/>
				</div>

				<div>
					<label for="acct-type" class="block text-xs font-medium text-ink-secondary mb-1">
						Account Type
					</label>
					<select
						id="acct-type"
						bind:value={type}
						onchange={handleTypeChange}
						class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-ink-primary focus:border-accent-pop focus:outline-none"
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

			{#if isTrackingType}
				<p
					data-testid="account-tracking-note"
					class="rounded-lg border border-border bg-subtle px-3 py-2 text-[11px] text-ink-secondary"
				>
					Tracked for balance and net worth — internal transactions stay out of your monthly budget review. Payments from checking or credit cards still count in your budget.
				</p>
			{/if}

			{#if isLinked && editingAccount}
				<div class="rounded-lg border border-border bg-subtle p-3.5 space-y-2.5">
					<div class="flex items-center justify-between">
						<div>
							<p class="text-xs font-medium text-ink-secondary">Current Balance</p>
							<p class="text-[11px] text-ink-muted">Synced automatically via SimpleFIN</p>
						</div>
						<span
							class="text-base font-mono font-semibold {editingAccount.currentBalance < 0
								? 'text-status-danger'
								: 'text-status-good'}"
						>
							{fmtMoney(editingAccount.currentBalance)}
						</span>
					</div>

					{#if !confirmUnlink}
						<div class="pt-1 border-t border-border flex justify-end">
							<button
								type="button"
								onclick={() => (confirmUnlink = true)}
								disabled={isSubmitting}
								class="text-xs text-status-warn hover:underline"
							>
								Unlink from SimpleFIN
							</button>
						</div>
					{:else}
						<div class="pt-2 border-t border-border space-y-2">
							<p class="text-xs text-status-warn">
								Convert to a manual account? SimpleFIN will stop syncing this account's balance and transactions.
							</p>
							<div class="flex justify-end gap-2">
								<button
									type="button"
									onclick={() => (confirmUnlink = false)}
									disabled={isSubmitting}
									class="rounded-md border border-border bg-subtle px-2.5 py-1 text-xs text-ink-secondary hover:bg-surface-hover hover:text-ink-primary transition-colors"
								>
									Cancel
								</button>
								<button
									type="button"
									onclick={handleUnlinkConfirmed}
									disabled={isSubmitting}
									class="rounded-md border border-status-warn/40 bg-status-warn-bg px-3 py-1 text-xs font-semibold text-status-warn hover:opacity-90 disabled:opacity-50 transition-opacity"
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
						<label for="acct-balance" class="block text-xs font-medium text-ink-secondary mb-1">
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
							class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm font-mono text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
						/>
						{#if isDebtAccount}
							<div class="mt-2 flex items-center justify-between gap-2">
								<label for="acct-credit-overpaid" class="flex items-center gap-2 text-xs text-ink-secondary cursor-pointer">
									<input
										id="acct-credit-overpaid"
										type="checkbox"
										bind:checked={isCreditOverpaid}
										class="rounded border-border bg-canvas accent-accent-pop"
									/>
									<span>Account has a positive / credit balance (overpaid)</span>
								</label>
								<span class="text-[11px] font-mono text-ink-muted">
									Stored: {fmtMoney(computedBalance)}
								</span>
							</div>
						{/if}
					</div>

					{#if hasBalanceChanged}
						<label
							for="acct-record-adj"
							class="flex items-start gap-2.5 rounded-lg border border-accent-pop/30 bg-accent-tint p-3 cursor-pointer"
						>
							<input
								id="acct-record-adj"
								type="checkbox"
								bind:checked={recordBalanceAdjustment}
								class="mt-0.5 rounded border-border bg-canvas accent-accent-pop"
							/>
							<div>
								<span class="block text-xs font-medium text-accent-pop">
									Record balance adjustment transaction ({fmtSigned(balanceDelta)})
								</span>
								<span class="block text-[11px] text-ink-secondary">
									Adds an uncategorized "Balance Adjustment" entry to the transaction ledger.
								</span>
							</div>
						</label>
					{/if}
				</div>
			{/if}

			{#if type === 'credit_card'}
				<div class="rounded-lg border border-border bg-subtle p-3.5 space-y-3">
					<p class="text-xs font-semibold uppercase tracking-wider text-ink-secondary">
						Credit Card Settings
					</p>

					<div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
						<div>
							<label for="acct-card-mode" class="block text-xs font-medium text-ink-secondary mb-1">
								Card Mode
							</label>
							<select
								id="acct-card-mode"
								bind:value={cardMode}
								class="w-full rounded-lg border border-border bg-canvas px-2.5 py-2 text-xs text-ink-primary focus:border-accent-pop focus:outline-none"
							>
								<option value="PAY_IN_FULL">Pay in Full</option>
								<option value="DEBT_PAYDOWN">Debt Paydown</option>
							</select>
						</div>

						<div>
							<label for="acct-apr" class="block text-xs font-medium text-ink-secondary mb-1">
								APR (%)
							</label>
							<input
								id="acct-apr"
								type="number"
								step="0.01"
								min="0"
								bind:value={aprInput}
								placeholder="0.00"
								class="w-full rounded-lg border border-border bg-canvas px-2.5 py-2 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
							/>
						</div>

						<div>
							<label for="acct-due-day" class="block text-xs font-medium text-ink-secondary mb-1">
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
								class="w-full rounded-lg border border-border bg-canvas px-2.5 py-2 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
							/>
						</div>
					</div>

					{#if cardMode === 'DEBT_PAYDOWN'}
						<div class="grid grid-cols-1 sm:grid-cols-2 gap-3 pt-1">
							<div>
								<label for="acct-debt-cat" class="block text-xs font-medium text-ink-secondary mb-1">
									Debt Payoff Category
								</label>
								<select
									id="acct-debt-cat"
									bind:value={debtPayoffCategoryId}
									required
									class="w-full rounded-lg border border-border bg-canvas px-2.5 py-2 text-xs text-ink-primary focus:border-accent-pop focus:outline-none"
								>
									<option value="">Select category...</option>
									{#each categories as cat (cat.id)}
										<option value={cat.id}>{cat.groupName} — {cat.name}</option>
									{/each}
								</select>
								{#if categories.length === 0}
									<p class="text-[11px] text-status-warn mt-1">
										No categories found. Create one on the Budget page first.
									</p>
								{/if}
							</div>

							<div>
								<label for="acct-debt-baseline" class="block text-xs font-medium text-ink-secondary mb-1">
									Initial Debt Baseline ($)
								</label>
								<input
									id="acct-debt-baseline"
									type="number"
									step="0.01"
									min="0"
									bind:value={initialDebtBaselineInput}
									placeholder="Defaults to owed balance"
									class="w-full rounded-lg border border-border bg-canvas px-2.5 py-2 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
								/>
							</div>
						</div>
					{/if}
				</div>
			{:else if type === 'loan'}
				<div>
					<label for="acct-apr" class="block text-xs font-medium text-ink-secondary mb-1">
						APR (%)
					</label>
					<input
						id="acct-apr"
						type="number"
						step="0.01"
						min="0"
						bind:value={aprInput}
						placeholder="Optional (e.g. 5.99)"
						class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
					/>
				</div>
			{/if}

			{#if errorMsg}
				<p class="text-xs text-status-danger bg-status-danger-bg border border-status-danger/30 rounded-lg px-3 py-2">
					{errorMsg}
				</p>
			{/if}

			{#if isEditing && confirmDelete}
				<div class="rounded-lg border border-status-danger/40 bg-status-danger-bg p-3 space-y-2">
					<p class="text-xs text-status-danger">
						Delete "{editingAccount?.name}" and all of its transactions? This cannot be undone.
					</p>
					<div class="flex items-center justify-end gap-2">
						<button
							type="button"
							onclick={handleDeleteConfirmed}
							disabled={isSubmitting}
							class="rounded-lg bg-status-danger px-3 py-1.5 text-xs font-semibold text-accent-primary-text hover:opacity-90 disabled:opacity-50 transition-opacity"
						>
							Confirm Delete
						</button>
						<button
							type="button"
							onclick={() => (confirmDelete = false)}
							disabled={isSubmitting}
							class="rounded-lg border border-border bg-subtle px-3 py-1.5 text-xs font-medium text-ink-secondary hover:bg-surface-hover hover:text-ink-primary transition-colors"
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
						class="rounded-lg border border-status-danger/40 px-3 py-2 text-xs font-medium text-status-danger hover:bg-status-danger-bg disabled:opacity-50 transition-colors"
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
						class="rounded-lg border border-border bg-subtle px-3.5 py-2 text-xs font-medium text-ink-secondary hover:bg-surface-hover hover:text-ink-primary transition-colors"
					>
						Cancel
					</button>
					<button
						type="submit"
						disabled={isSubmitting}
						class="rounded-lg bg-accent-primary px-4 py-2 text-xs font-semibold text-accent-primary-text hover:opacity-90 disabled:opacity-50 transition-opacity"
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
