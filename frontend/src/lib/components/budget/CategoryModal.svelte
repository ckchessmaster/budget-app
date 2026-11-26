<script lang="ts">
	import {
		createCategory,
		deleteCategory,
		updateCategory,
		updateMonthlyPlanned
	} from '$lib/api/budget';
	import type { CategoryBudget, RolloverMode, TargetCadence } from '$lib/types/budget';
	import type { AccountItem } from '$lib/types/transaction';

	interface Props {
		isOpen: boolean;
		editingCategory: CategoryBudget | null;
		allCategories: CategoryBudget[];
		accounts?: AccountItem[];
		availableGroups?: string[];
		defaultGroupName?: string;
		yearMonth?: string;
		onClose: () => void;
		onSuccess: () => void;
	}

	let {
		isOpen,
		editingCategory,
		allCategories,
		accounts = [],
		availableGroups = [],
		defaultGroupName = 'Essentials',
		yearMonth,
		onClose,
		onSuccess
	}: Props = $props();

	let name = $state('');
	let groupName = $state('');
	let monthlyBudget = $state('0.00');
	let itemKind = $state<'EXPENSE' | 'FUND'>('EXPENSE');
	let capAtGoal = $state(false);
	let sweepTargetCategoryId = $state<string>('');
	let heldInAccountId = $state<string>('');
	let targetCadence = $state<TargetCadence>('NONE');
	let targetAmount = $state('');
	let targetDate = $state('');
	let startingBalance = $state('');
	let isSubmitting = $state(false);
	let errorMsg = $state('');

	let cashAccounts = $derived(
		accounts.filter((a) => a.type === 'checking' || a.type === 'savings' || a.type === 'cash')
	);

	$effect(() => {
		if (isOpen) {
			if (editingCategory) {
				name = editingCategory.name;
				groupName = editingCategory.groupName;
				monthlyBudget = editingCategory.originalPlanned.toFixed(2);
				itemKind = editingCategory.rolloverMode === 'SWEEP' ? 'EXPENSE' : 'FUND';
				capAtGoal = editingCategory.rolloverMode === 'TARGET_CAP';
				sweepTargetCategoryId = editingCategory.sweepTargetCategoryId ?? '';
				heldInAccountId = editingCategory.heldInAccountId ?? '';
				targetCadence = editingCategory.targetCadence ?? 'NONE';
				targetAmount =
					editingCategory.targetAmount != null ? editingCategory.targetAmount.toFixed(2) : '';
				targetDate = editingCategory.targetDate ?? '';
				startingBalance =
					editingCategory.carryoverBalance != null
						? editingCategory.carryoverBalance.toFixed(2)
						: '0.00';
			} else {
				name = '';
				groupName = defaultGroupName?.trim() || 'Essentials';
				monthlyBudget = '0.00';
				itemKind = 'EXPENSE';
				capAtGoal = false;
				sweepTargetCategoryId = '';
				heldInAccountId = '';
				targetCadence = 'NONE';
				targetAmount = '';
				targetDate = '';
				startingBalance = '';
			}
			errorMsg = '';
		}
	});

	let sweepCandidates = $derived(
		allCategories.filter((c) => !editingCategory || c.id !== editingCategory.id)
	);

	let groupOptions = $derived.by(() => {
		const seen = new Set<string>();
		const out: string[] = [];
		for (const g of [...availableGroups, groupName, defaultGroupName]) {
			const trimmed = g?.trim();
			if (trimmed && !seen.has(trimmed)) {
				seen.add(trimmed);
				out.push(trimmed);
			}
		}
		return out;
	});

	let resolvedRolloverMode = $derived<RolloverMode>(
		itemKind === 'EXPENSE'
			? 'SWEEP'
			: capAtGoal && Number(targetAmount) > 0
				? 'TARGET_CAP'
				: 'ROLLOVER_BALANCE'
	);

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		isSubmitting = true;
		errorMsg = '';

		const plannedVal = Number(monthlyBudget) || 0;
		const nextSortOrder =
			editingCategory?.sortOrder ??
			Math.max(-1, ...allCategories.map((c) => c.sortOrder)) + 1;
		const startingStr = String(startingBalance ?? '').trim();
		const parsedStarting = startingStr === '' ? null : Number(startingStr);
		if (parsedStarting !== null && (!Number.isFinite(parsedStarting) || parsedStarting < 0)) {
			errorMsg = 'Starting balance must be 0 or greater.';
			isSubmitting = false;
			return;
		}

		const payload = {
			name: name.trim(),
			group_name: groupName.trim(),
			monthly_budget: plannedVal,
			rollover_mode: resolvedRolloverMode,
			sweep_target_category_id:
				resolvedRolloverMode === 'SWEEP' && sweepTargetCategoryId ? sweepTargetCategoryId : null,
			held_in_account_id: itemKind === 'FUND' && heldInAccountId ? heldInAccountId : null,
			target_cadence: itemKind === 'FUND' ? targetCadence : 'NONE',
			target_amount: itemKind === 'FUND' && targetAmount ? Number(targetAmount) : null,
			target_date: itemKind === 'FUND' && targetDate ? targetDate : null,
			sort_order: nextSortOrder,
			...(itemKind === 'FUND' && parsedStarting !== null
				? {
						starting_balance: Math.round(parsedStarting * 100) / 100,
						...(yearMonth ? { year_month: yearMonth } : {})
					}
				: {})
		};

		try {
			if (editingCategory) {
				await updateCategory(editingCategory.id, payload);
				if (yearMonth) {
					await updateMonthlyPlanned(editingCategory.id, yearMonth, plannedVal);
				}
			} else {
				await createCategory(payload);
			}
			onSuccess();
		} catch (err: any) {
			errorMsg = err?.message || 'Failed to save category.';
		} finally {
			isSubmitting = false;
		}
	}

	async function handleDelete() {
		if (!editingCategory) return;
		isSubmitting = true;
		errorMsg = '';
		try {
			await deleteCategory(editingCategory.id);
			onSuccess();
		} catch (err: any) {
			errorMsg = err?.message || 'Failed to delete category.';
		} finally {
			isSubmitting = false;
		}
	}
</script>

{#if isOpen}
	<div class="fixed inset-0 z-50 flex items-center justify-center p-4">
		<button
			type="button"
			aria-label="Close category modal"
			onclick={onClose}
			class="fixed inset-0 bg-black/50 backdrop-blur-xs"
		></button>

		<div
			role="dialog"
			aria-modal="true"
			class="relative z-10 w-full max-w-md rounded-2xl border border-border bg-surface text-ink-primary p-6 shadow-2xl max-h-[90dvh] overflow-y-auto"
		>
			<div class="flex items-center justify-between border-b border-border pb-3">
				<h2 class="text-base font-semibold text-ink-primary">
					{editingCategory ? 'Edit Budget Item' : 'New Budget Item'}
				</h2>
				<button
					type="button"
					onclick={onClose}
					class="rounded-lg p-1 text-ink-muted hover:bg-surface-hover hover:text-ink-primary transition-colors"
				>
					✕
				</button>
			</div>

			<form onsubmit={handleSubmit} class="mt-4 space-y-3.5">
				<div class="grid grid-cols-2 gap-3">
					<div>
						<label for="cat-name" class="block text-xs font-medium text-ink-secondary mb-1">
							Item Name
						</label>
						<input
							id="cat-name"
							type="text"
							bind:value={name}
							required
							class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
						/>
					</div>
					<div>
						<label for="cat-group" class="block text-xs font-medium text-ink-secondary mb-1">
							Category
						</label>
						{#if groupOptions.length > 0}
							<select
								id="cat-group"
								bind:value={groupName}
								required
								class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-ink-primary focus:border-accent-pop focus:outline-none"
							>
								{#each groupOptions as grp (grp)}
									<option value={grp}>{grp}</option>
								{/each}
							</select>
						{:else}
							<input
								id="cat-group"
								type="text"
								bind:value={groupName}
								required
								class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
							/>
						{/if}
					</div>
				</div>

				<!-- Type Switch: Monthly Expense vs Fund / Savings Bucket -->
				<div class="space-y-1.5">
					<span class="block text-xs font-medium text-ink-secondary">Budget Type</span>
					<div class="grid grid-cols-2 gap-1.5 rounded-xl border border-border bg-subtle p-1">
						<button
							type="button"
							data-testid="cat-kind-expense"
							onclick={() => (itemKind = 'EXPENSE')}
							class="rounded-lg px-3 py-1.5 text-xs font-semibold transition-colors {itemKind === 'EXPENSE'
								? 'bg-surface text-ink-primary shadow-xs border border-border'
								: 'text-ink-secondary hover:text-ink-primary'}"
						>
							Monthly Expense
						</button>
						<button
							type="button"
							data-testid="cat-kind-fund"
							onclick={() => (itemKind = 'FUND')}
							class="rounded-lg px-3 py-1.5 text-xs font-semibold transition-colors {itemKind === 'FUND'
								? 'bg-surface text-accent-pop shadow-xs border border-accent-pop/30'
								: 'text-ink-secondary hover:text-ink-primary'}"
						>
							Fund / Savings Bucket
						</button>
					</div>
					<p class="text-[11px] text-ink-muted">
						{#if itemKind === 'EXPENSE'}
							Resets each month — unspent funds return to Ready to Assign (or sweep to a chosen bucket).
						{:else}
							Carries a running balance month to month — reserve money here and spend from it anytime.
						{/if}
					</p>
				</div>

				<div class="grid {itemKind === 'FUND' ? 'grid-cols-2' : 'grid-cols-1'} gap-3">
					<div>
						<label for="cat-budget" class="block text-xs font-medium text-ink-secondary mb-1">
							{itemKind === 'FUND' ? 'Monthly Contribution ($)' : 'Monthly Budget ($)'}
						</label>
						<input
							id="cat-budget"
							type="number"
							step="0.01"
							min="0"
							bind:value={monthlyBudget}
							required
							class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
						/>
					</div>

					{#if itemKind === 'FUND'}
						<div>
							<label for="cat-starting-balance" class="block text-xs font-medium text-ink-secondary mb-1">
								{editingCategory ? 'Prior Saved Balance ($)' : 'Starting Balance ($)'}
							</label>
							<input
								id="cat-starting-balance"
								type="number"
								step="0.01"
								min="0"
								placeholder="0.00"
								bind:value={startingBalance}
								class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
							/>
						</div>
					{/if}
				</div>

				{#if itemKind === 'EXPENSE' && sweepCandidates.length > 0}
					<div>
						<label for="cat-sweep-target" class="flex items-center gap-1 text-xs font-medium text-ink-secondary mb-1">
							<span>Sweep Leftover To (optional)</span>
						</label>
						<select
							id="cat-sweep-target"
							bind:value={sweepTargetCategoryId}
							class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-ink-primary focus:border-accent-pop focus:outline-none"
						>
							<option value="">Ready to Assign</option>
							{#each sweepCandidates as cand}
								<option value={cand.id}>{cand.groupName} — {cand.name}</option>
							{/each}
						</select>
					</div>
				{/if}

				{#if itemKind === 'FUND'}
					<div>
						<label for="cat-held-account" class="block text-xs font-medium text-ink-secondary mb-1">
							Held in Account <span class="text-ink-muted font-normal">(optional)</span>
						</label>
						<select
							id="cat-held-account"
							bind:value={heldInAccountId}
							class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-ink-primary focus:border-accent-pop focus:outline-none"
						>
							<option value="">None (virtual bucket across all cash)</option>
							{#each cashAccounts as acct (acct.id)}
								<option value={acct.id}>{acct.name}</option>
							{/each}
						</select>
						<p class="mt-1 text-[11px] text-ink-muted">
							Track how much of a checking or savings account is reserved for this fund.
						</p>
					</div>

					<div class="grid grid-cols-3 gap-2">
						<div>
							<label for="cat-cadence" class="block text-xs font-medium text-ink-secondary mb-1">
								Goal Cadence
							</label>
							<select
								id="cat-cadence"
								bind:value={targetCadence}
								class="w-full rounded-lg border border-border bg-canvas px-2 py-2 text-xs text-ink-primary focus:border-accent-pop focus:outline-none"
							>
								<option value="NONE">None</option>
								<option value="MONTHLY">Monthly</option>
								<option value="ANNUAL">Annual</option>
								<option value="TARGET_DATE">Target Date</option>
							</select>
						</div>
						<div>
							<label for="cat-target-amt" class="block text-xs font-medium text-ink-secondary mb-1">
								Goal Target ($)
							</label>
							<input
								id="cat-target-amt"
								type="number"
								step="0.01"
								min="0"
								bind:value={targetAmount}
								placeholder="Optional"
								class="w-full rounded-lg border border-border bg-canvas px-2.5 py-2 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
							/>
						</div>
						<div>
							<label for="cat-target-date" class="block text-xs font-medium text-ink-secondary mb-1">
								Target Date
							</label>
							<input
								id="cat-target-date"
								type="date"
								bind:value={targetDate}
								class="w-full rounded-lg border border-border bg-canvas px-2 py-2 text-xs text-ink-primary focus:border-accent-pop focus:outline-none"
							/>
						</div>
					</div>
				{/if}

				{#if itemKind === 'FUND' && Number(targetAmount) > 0}
					<label
						for="cat-cap-at-goal"
						class="flex items-start gap-2 rounded-lg border border-border bg-subtle px-3 py-2 cursor-pointer"
					>
						<input
							id="cat-cap-at-goal"
							type="checkbox"
							bind:checked={capAtGoal}
							class="mt-0.5 rounded border-border bg-canvas accent-accent-pop"
						/>
						<span class="text-[11px] text-ink-secondary">
							<strong class="text-ink-primary">Cap balance at goal target</strong> — stop adding monthly contributions once the fund reaches ${Number(targetAmount).toFixed(2)}.
						</span>
					</label>
				{/if}

				{#if errorMsg}
					<p class="text-xs text-status-danger bg-status-danger-bg border border-status-danger/30 rounded-lg px-3 py-2">
						{errorMsg}
					</p>
				{/if}

				<div class="flex items-center justify-between pt-3">
					{#if editingCategory}
						<button
							type="button"
							onclick={handleDelete}
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
							onclick={onClose}
							class="rounded-lg border border-border bg-subtle px-3.5 py-2 text-xs font-medium text-ink-secondary hover:bg-surface-hover hover:text-ink-primary transition-colors"
						>
							Cancel
						</button>
						<button
							type="submit"
							disabled={isSubmitting}
							class="rounded-lg bg-accent-primary px-4 py-2 text-xs font-semibold text-accent-primary-text hover:opacity-90 disabled:opacity-50 transition-opacity"
						>
							{isSubmitting ? 'Saving...' : 'Save Item'}
						</button>
					</div>
				</div>
			</form>
		</div>
	</div>
{/if}
