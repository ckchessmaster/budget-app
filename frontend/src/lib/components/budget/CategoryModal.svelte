<script lang="ts">
	import { createCategory, deleteCategory, updateCategory } from '$lib/api/budget';
	import type { CategoryBudget, RolloverMode, TargetCadence } from '$lib/types/budget';

	interface Props {
		isOpen: boolean;
		editingCategory: CategoryBudget | null;
		allCategories: CategoryBudget[];
		onClose: () => void;
		onSuccess: () => void;
	}

	let { isOpen, editingCategory, allCategories, onClose, onSuccess }: Props = $props();

	let name = $state('');
	let groupName = $state('');
	let monthlyBudget = $state('0.00');
	let rolloverMode = $state<RolloverMode>('SWEEP');
	let sweepTargetCategoryId = $state<string>('');
	let targetCadence = $state<TargetCadence>('NONE');
	let targetAmount = $state('');
	let targetDate = $state('');
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
				targetAmount = editingCategory.targetAmount != null ? editingCategory.targetAmount.toFixed(2) : '';
				targetDate = editingCategory.targetDate ?? '';
			} else {
				name = '';
				groupName = 'Essentials';
				monthlyBudget = '0.00';
				rolloverMode = 'SWEEP';
				sweepTargetCategoryId = '';
				targetCadence = 'NONE';
				targetAmount = '';
				targetDate = '';
			}
			errorMsg = '';
		}
	});

	let sweepCandidates = $derived(
		allCategories.filter((c) => !editingCategory || c.id !== editingCategory.id)
	);

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		isSubmitting = true;
		errorMsg = '';

		const payload = {
			name: name.trim(),
			group_name: groupName.trim(),
			monthly_budget: Number(monthlyBudget) || 0,
			rollover_mode: rolloverMode,
			sweep_target_category_id:
				rolloverMode === 'SWEEP' && sweepTargetCategoryId ? sweepTargetCategoryId : null,
			target_cadence: targetCadence,
			target_amount: targetAmount ? Number(targetAmount) : null,
			target_date: targetDate ? targetDate : null,
			sort_order: editingCategory?.sortOrder ?? 0
		};

		try {
			if (editingCategory) {
				await updateCategory(editingCategory.id, payload);
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
			class="fixed inset-0 bg-black/60 backdrop-blur-xs"
		></button>

		<div
			role="dialog"
			aria-modal="true"
			class="relative z-10 w-full max-w-md rounded-2xl border border-slate-800 bg-slate-900 p-6 shadow-2xl max-h-[90dvh] overflow-y-auto"
		>
			<div class="flex items-center justify-between border-b border-slate-800 pb-3">
				<h2 class="text-base font-semibold text-white">
					{editingCategory ? 'Edit Category' : 'New Category'}
				</h2>
				<button
					type="button"
					onclick={onClose}
					class="rounded-lg p-1 text-slate-400 hover:bg-slate-800 hover:text-white"
				>
					✕
				</button>
			</div>

			<form onsubmit={handleSubmit} class="mt-4 space-y-3.5">
				<div class="grid grid-cols-2 gap-3">
					<div>
						<label for="cat-name" class="block text-xs font-medium text-slate-300 mb-1">
							Name
						</label>
						<input
							id="cat-name"
							type="text"
							bind:value={name}
							required
							class="w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white focus:border-emerald-500 focus:outline-none"
						/>
					</div>
					<div>
						<label for="cat-group" class="block text-xs font-medium text-slate-300 mb-1">
							Group
						</label>
						<input
							id="cat-group"
							type="text"
							bind:value={groupName}
							required
							class="w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white focus:border-emerald-500 focus:outline-none"
						/>
					</div>
				</div>

				<div class="grid grid-cols-2 gap-3">
					<div>
						<label for="cat-budget" class="block text-xs font-medium text-slate-300 mb-1">
							Monthly Budget ($)
						</label>
						<input
							id="cat-budget"
							type="number"
							step="0.01"
							min="0"
							bind:value={monthlyBudget}
							required
							class="w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white focus:border-emerald-500 focus:outline-none"
						/>
					</div>
					<div>
						<label for="cat-rollover" class="block text-xs font-medium text-slate-300 mb-1">
							Rollover Mode
						</label>
						<select
							id="cat-rollover"
							bind:value={rolloverMode}
							class="w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white focus:border-emerald-500 focus:outline-none"
						>
							<option value="SWEEP">Sweep</option>
							<option value="ROLLOVER_BALANCE">Rollover Balance</option>
							<option value="TARGET_CAP">Target Cap</option>
						</select>
					</div>
				</div>

				{#if rolloverMode === 'SWEEP' && sweepCandidates.length > 0}
					<div>
						<label for="cat-sweep-target" class="block text-xs font-medium text-slate-300 mb-1">
							Sweep Target Category (optional)
						</label>
						<select
							id="cat-sweep-target"
							bind:value={sweepTargetCategoryId}
							class="w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white focus:border-emerald-500 focus:outline-none"
						>
							<option value="">None (return to Ready to Assign)</option>
							{#each sweepCandidates as cand}
								<option value={cand.id}>{cand.groupName} — {cand.name}</option>
							{/each}
						</select>
					</div>
				{/if}

				<div class="grid grid-cols-3 gap-2">
					<div>
						<label for="cat-cadence" class="block text-xs font-medium text-slate-300 mb-1">
							Goal Cadence
						</label>
						<select
							id="cat-cadence"
							bind:value={targetCadence}
							class="w-full rounded-lg border border-slate-700 bg-slate-950 px-2 py-2 text-xs text-white focus:border-emerald-500 focus:outline-none"
						>
							<option value="NONE">None</option>
							<option value="MONTHLY">Monthly</option>
							<option value="ANNUAL">Annual</option>
							<option value="TARGET_DATE">Target Date</option>
						</select>
					</div>
					<div>
						<label for="cat-target-amt" class="block text-xs font-medium text-slate-300 mb-1">
							Target ($)
						</label>
						<input
							id="cat-target-amt"
							type="number"
							step="0.01"
							min="0"
							bind:value={targetAmount}
							placeholder="Optional"
							class="w-full rounded-lg border border-slate-700 bg-slate-950 px-2.5 py-2 text-xs text-white focus:border-emerald-500 focus:outline-none"
						/>
					</div>
					<div>
						<label for="cat-target-date" class="block text-xs font-medium text-slate-300 mb-1">
							Target Date
						</label>
						<input
							id="cat-target-date"
							type="date"
							bind:value={targetDate}
							class="w-full rounded-lg border border-slate-700 bg-slate-950 px-2 py-2 text-xs text-white focus:border-emerald-500 focus:outline-none"
						/>
					</div>
				</div>

				{#if errorMsg}
					<p class="text-xs text-rose-400 bg-rose-500/10 border border-rose-500/30 rounded-lg px-3 py-2">
						{errorMsg}
					</p>
				{/if}

				<div class="flex items-center justify-between pt-3">
					{#if editingCategory}
						<button
							type="button"
							onclick={handleDelete}
							disabled={isSubmitting}
							class="rounded-lg border border-rose-500/40 px-3 py-2 text-xs font-medium text-rose-400 hover:bg-rose-500/10"
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
							class="rounded-lg border border-slate-700 px-3.5 py-2 text-xs font-medium text-slate-300 hover:bg-slate-800"
						>
							Cancel
						</button>
						<button
							type="submit"
							disabled={isSubmitting}
							class="rounded-lg bg-emerald-600 px-4 py-2 text-xs font-semibold text-white hover:bg-emerald-500 disabled:opacity-50"
						>
							{isSubmitting ? 'Saving...' : 'Save Category'}
						</button>
					</div>
				</div>
			</form>
		</div>
	</div>
{/if}
