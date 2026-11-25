<script lang="ts">
	import {
		createCategory,
		deleteCategory,
		updateCategory,
		updateMonthlyPlanned
	} from '$lib/api/budget';
	import type { CategoryBudget, RolloverMode, TargetCadence } from '$lib/types/budget';

	interface Props {
		isOpen: boolean;
		editingCategory: CategoryBudget | null;
		allCategories: CategoryBudget[];
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
		availableGroups = [],
		defaultGroupName = 'Essentials',
		yearMonth,
		onClose,
		onSuccess
	}: Props = $props();

	let name = $state('');
	let groupName = $state('');
	let monthlyBudget = $state('0.00');
	let rolloverMode = $state<RolloverMode>('SWEEP');
	let sweepTargetCategoryId = $state<string>('');
	let targetCadence = $state<TargetCadence>('NONE');
	let targetAmount = $state('');
	let targetDate = $state('');
	let startingBalance = $state('');
	let isSubmitting = $state(false);
	let errorMsg = $state('');

	$effect(() => {
		if (isOpen) {
			if (editingCategory) {
				name = editingCategory.name;
				groupName = editingCategory.groupName;
				monthlyBudget = editingCategory.originalPlanned.toFixed(2);
				rolloverMode = editingCategory.rolloverMode;
				sweepTargetCategoryId = editingCategory.sweepTargetCategoryId ?? '';
				targetCadence = editingCategory.targetCadence ?? 'NONE';
				targetAmount =
					editingCategory.targetAmount != null ? editingCategory.targetAmount.toFixed(2) : '';
				targetDate = editingCategory.targetDate ?? '';
			} else {
				name = '';
				groupName = defaultGroupName?.trim() || 'Essentials';
				monthlyBudget = '0.00';
				rolloverMode = 'SWEEP';
				sweepTargetCategoryId = '';
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

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		isSubmitting = true;
		errorMsg = '';

		const plannedVal = Number(monthlyBudget) || 0;
		const nextSortOrder =
			editingCategory?.sortOrder ??
			Math.max(-1, ...allCategories.map((c) => c.sortOrder)) + 1;
		const startingVal = Number(startingBalance);
		const payload = {
			name: name.trim(),
			group_name: groupName.trim(),
			monthly_budget: plannedVal,
			rollover_mode: rolloverMode,
			sweep_target_category_id:
				rolloverMode === 'SWEEP' && sweepTargetCategoryId ? sweepTargetCategoryId : null,
			target_cadence: targetCadence,
			target_amount: targetAmount ? Number(targetAmount) : null,
			target_date: targetDate ? targetDate : null,
			sort_order: nextSortOrder,
			...(!editingCategory && Number.isFinite(startingVal) && startingVal > 0
				? {
						starting_balance: startingVal,
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

				<div class="grid grid-cols-2 gap-3">
					<div>
						<label for="cat-budget" class="block text-xs font-medium text-ink-secondary mb-1">
							Monthly Budget ($)
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
					<div>
						<label for="cat-rollover" class="flex items-center gap-1 text-xs font-medium text-ink-secondary mb-1">
							<span>Rollover Mode</span>
							<span
								title="Sweep: leftover funds at month-end return to Ready to Assign (or a target category). Rollover Balance: leftover funds stay in this category next month. Target Cap: leftover funds roll over up to the monthly budget cap."
								aria-label="Rollover Mode explanation"
								class="inline-flex items-center justify-center w-3.5 h-3.5 rounded-full border border-border text-[10px] text-ink-muted cursor-help"
							>?</span>
						</label>
						<select
							id="cat-rollover"
							bind:value={rolloverMode}
							class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-ink-primary focus:border-accent-pop focus:outline-none"
						>
							<option value="SWEEP">Sweep</option>
							<option value="ROLLOVER_BALANCE">Rollover Balance</option>
							<option value="TARGET_CAP">Target Cap</option>
						</select>
						<p class="mt-1 text-[11px] text-ink-muted">
							{#if rolloverMode === 'SWEEP'}
								Leftover funds at month-end sweep out of this category.
							{:else if rolloverMode === 'ROLLOVER_BALANCE'}
								Leftover funds carry forward into next month's balance.
							{:else}
								Leftover funds carry forward up to the monthly budget cap.
							{/if}
						</p>
					</div>
				</div>

				{#if rolloverMode === 'SWEEP' && sweepCandidates.length > 0}
					<div>
						<label for="cat-sweep-target" class="flex items-center gap-1 text-xs font-medium text-ink-secondary mb-1">
							<span>Sweep Target Category (optional)</span>
							<span
								title="Where unspent funds go at month-end when Sweep is active. Leave as None to return them to Ready to Assign."
								aria-label="Sweep Target Category explanation"
								class="inline-flex items-center justify-center w-3.5 h-3.5 rounded-full border border-border text-[10px] text-ink-muted cursor-help"
							>?</span>
						</label>
						<select
							id="cat-sweep-target"
							bind:value={sweepTargetCategoryId}
							class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-ink-primary focus:border-accent-pop focus:outline-none"
						>
							<option value="">None (return to Ready to Assign)</option>
							{#each sweepCandidates as cand}
								<option value={cand.id}>{cand.groupName} — {cand.name}</option>
							{/each}
						</select>
					</div>
				{/if}

				{#if !editingCategory && rolloverMode !== 'SWEEP'}
					<div>
						<label for="cat-starting-balance" class="block text-xs font-medium text-ink-secondary mb-1">
							Starting Balance ($) <span class="text-ink-muted font-normal">(optional)</span>
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
						<p class="mt-1 text-[11px] text-ink-muted">
							Sets the initial carryover balance for this sinking fund in the current month.
						</p>
					</div>
				{/if}

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
							Target ($)
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
