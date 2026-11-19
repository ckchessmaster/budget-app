<script lang="ts">
	import { createPlannedIncome, deletePlannedIncome } from '$lib/api/budget';
	import type { PlannedIncome } from '$lib/types/budget';

	interface Props {
		isOpen: boolean;
		yearMonth: string; // YYYY-MM-01
		incomes: PlannedIncome[];
		onClose: () => void;
		onChanged: () => void;
	}

	let { isOpen, yearMonth, incomes, onClose, onChanged }: Props = $props();

	let name = $state('');
	let amount = $state('');
	let isSubmitting = $state(false);
	let errorMsg = $state('');

	async function handleAdd(e: SubmitEvent) {
		e.preventDefault();
		const parsed = Number(amount);
		if (!Number.isFinite(parsed) || parsed <= 0) {
			errorMsg = 'Amount must be greater than $0.00.';
			return;
		}
		isSubmitting = true;
		errorMsg = '';
		try {
			await createPlannedIncome({
				year_month: yearMonth,
				name: name.trim(),
				amount: parsed
			});
			name = '';
			amount = '';
			onChanged();
		} catch (err: any) {
			errorMsg = err?.message || 'Failed to add planned income.';
		} finally {
			isSubmitting = false;
		}
	}

	async function handleDelete(id: string) {
		isSubmitting = true;
		errorMsg = '';
		try {
			await deletePlannedIncome(id);
			onChanged();
		} catch (err: any) {
			errorMsg = err?.message || 'Failed to delete planned income.';
		} finally {
			isSubmitting = false;
		}
	}
</script>

{#if isOpen}
	<div class="fixed inset-0 z-50 flex items-center justify-center p-4">
		<button
			type="button"
			aria-label="Close incomes modal"
			onclick={onClose}
			class="fixed inset-0 bg-black/50 dark:bg-black/60 backdrop-blur-xs"
		></button>

		<div
			role="dialog"
			aria-modal="true"
			class="relative z-10 w-full max-w-md rounded-2xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 p-6 shadow-2xl max-h-[90dvh] overflow-y-auto"
		>
			<div class="flex items-center justify-between border-b border-slate-200 dark:border-slate-800 pb-3">
				<h2 class="text-base font-semibold text-slate-900 dark:text-white">Planned Monthly Income</h2>
				<button
					type="button"
					onclick={onClose}
					class="rounded-lg p-1 text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800 hover:text-slate-700 dark:hover:text-white"
				>
					✕
				</button>
			</div>

			<div class="mt-4 space-y-2">
				{#if incomes.length === 0}
					<p class="text-xs text-slate-500 dark:text-slate-400 py-2">
						No planned incomes recorded for this month yet.
					</p>
				{:else}
					{#each incomes as inc}
						<div class="flex items-center justify-between rounded-lg border border-slate-200 dark:border-slate-800 bg-slate-50 dark:bg-slate-950 px-3 py-2">
							<div>
								<p class="text-sm font-medium text-slate-900 dark:text-slate-100">{inc.name}</p>
								<p class="text-xs text-emerald-600 dark:text-emerald-400">${inc.amount.toFixed(2)}</p>
							</div>
							<button
								type="button"
								disabled={isSubmitting}
								onclick={() => handleDelete(inc.id)}
								class="text-xs text-rose-600 dark:text-rose-400 hover:text-rose-700 dark:hover:text-rose-300 px-2 py-1 rounded hover:bg-rose-50 dark:hover:bg-rose-500/10"
							>
								Remove
							</button>
						</div>
					{/each}
				{/if}
			</div>

			<form onsubmit={handleAdd} class="mt-5 border-t border-slate-200 dark:border-slate-800 pt-4 space-y-3">
				<p class="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400">
					Add Income Source
				</p>
				<div class="grid grid-cols-5 gap-2">
					<input
						type="text"
						placeholder="Paycheck 1"
						bind:value={name}
						required
						class="col-span-3 rounded-lg border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-3 py-2 text-sm text-slate-900 dark:text-white focus:border-emerald-500 focus:outline-none"
					/>
					<input
						type="number"
						step="0.01"
						min="0.01"
						placeholder="2500.00"
						bind:value={amount}
						required
						class="col-span-2 rounded-lg border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-3 py-2 text-sm text-slate-900 dark:text-white focus:border-emerald-500 focus:outline-none"
					/>
				</div>

				{#if errorMsg}
					<p class="text-xs text-rose-700 dark:text-rose-400 bg-rose-50 dark:bg-rose-500/10 border border-rose-200 dark:border-rose-500/30 rounded-lg px-3 py-2">
						{errorMsg}
					</p>
				{/if}

				<div class="flex justify-end gap-2">
					<button
						type="button"
						onclick={onClose}
						class="rounded-lg border border-slate-300 dark:border-slate-700 px-3.5 py-2 text-xs font-medium text-slate-700 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800"
					>
						Done
					</button>
					<button
						type="submit"
						disabled={isSubmitting}
						class="rounded-lg bg-emerald-600 px-4 py-2 text-xs font-semibold text-white hover:bg-emerald-500 disabled:opacity-50"
					>
						{isSubmitting ? 'Adding...' : 'Add Income'}
					</button>
				</div>
			</form>
		</div>
	</div>
{/if}
