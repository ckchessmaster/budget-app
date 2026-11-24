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
			class="fixed inset-0 bg-black/50 backdrop-blur-xs"
		></button>

		<div
			role="dialog"
			aria-modal="true"
			class="relative z-10 w-full max-w-md rounded-2xl border border-border bg-surface text-ink-primary p-6 shadow-2xl max-h-[90dvh] overflow-y-auto"
		>
			<div class="flex items-center justify-between border-b border-border pb-3">
				<h2 class="text-base font-semibold text-ink-primary">Planned Monthly Income</h2>
				<button
					type="button"
					onclick={onClose}
					class="rounded-lg p-1 text-ink-muted hover:bg-surface-hover hover:text-ink-primary transition-colors"
				>
					✕
				</button>
			</div>

			<div class="mt-4 space-y-2">
				{#if incomes.length === 0}
					<p class="text-xs text-ink-secondary py-2">
						No planned incomes recorded for this month yet.
					</p>
				{:else}
					{#each incomes as inc}
						<div class="flex items-center justify-between rounded-lg border border-border bg-subtle px-3 py-2">
							<div>
								<p class="text-sm font-medium text-ink-primary">{inc.name}</p>
								<p class="text-xs font-mono text-status-good">${inc.amount.toFixed(2)}</p>
							</div>
							<button
								type="button"
								disabled={isSubmitting}
								onclick={() => handleDelete(inc.id)}
								class="text-xs text-status-danger hover:bg-status-danger-bg px-2 py-1 rounded transition-colors disabled:opacity-50"
							>
								Remove
							</button>
						</div>
					{/each}
				{/if}
			</div>

			<form onsubmit={handleAdd} class="mt-5 border-t border-border pt-4 space-y-3">
				<p class="text-xs font-semibold uppercase tracking-wider text-ink-secondary">
					Add Income Source
				</p>
				<div class="grid grid-cols-5 gap-2">
					<input
						type="text"
						placeholder="Paycheck 1"
						bind:value={name}
						required
						class="col-span-3 rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
					/>
					<input
						type="number"
						step="0.01"
						min="0.01"
						placeholder="2500.00"
						bind:value={amount}
						required
						class="col-span-2 rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
					/>
				</div>

				{#if errorMsg}
					<p class="text-xs text-status-danger bg-status-danger-bg border border-status-danger/30 rounded-lg px-3 py-2">
						{errorMsg}
					</p>
				{/if}

				<div class="flex justify-end gap-2">
					<button
						type="button"
						onclick={onClose}
						class="rounded-lg border border-border bg-subtle px-3.5 py-2 text-xs font-medium text-ink-secondary hover:bg-surface-hover hover:text-ink-primary transition-colors"
					>
						Done
					</button>
					<button
						type="submit"
						disabled={isSubmitting}
						class="rounded-lg bg-accent-primary px-4 py-2 text-xs font-semibold text-accent-primary-text hover:opacity-90 disabled:opacity-50 transition-opacity"
					>
						{isSubmitting ? 'Adding...' : 'Add Income'}
					</button>
				</div>
			</form>
		</div>
	</div>
{/if}
