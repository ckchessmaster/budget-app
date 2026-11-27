<script lang="ts">
	import {
		createPlannedIncome,
		deletePlannedIncome,
		updatePlannedIncome
	} from '$lib/api/budget';
	import type { PlannedIncome } from '$lib/types/budget';

	interface Props {
		isOpen: boolean;
		yearMonth: string; // YYYY-MM-01
		editingIncome: PlannedIncome | null;
		onClose: () => void;
		onSuccess: () => void;
	}

	let { isOpen, yearMonth, editingIncome, onClose, onSuccess }: Props = $props();

	let name = $state('');
	let amount = $state('0.00');
	let isSubmitting = $state(false);
	let errorMsg = $state('');

	$effect(() => {
		if (isOpen) {
			if (editingIncome) {
				name = editingIncome.name;
				amount = editingIncome.amount.toFixed(2);
			} else {
				name = '';
				amount = '';
			}
			errorMsg = '';
		}
	});

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		const trimmed = name.trim();
		const parsed = Number(amount);
		if (!trimmed || !Number.isFinite(parsed) || parsed < 0) {
			errorMsg = 'Enter a source name and an amount of $0.00 or greater.';
			return;
		}

		isSubmitting = true;
		errorMsg = '';
		try {
			if (editingIncome) {
				await updatePlannedIncome(editingIncome.id, {
					name: trimmed,
					amount: Math.round(parsed * 100) / 100
				});
			} else {
				await createPlannedIncome({
					year_month: yearMonth,
					name: trimmed,
					amount: Math.round(parsed * 100) / 100
				});
			}
			onSuccess();
		} catch (err: any) {
			errorMsg = err?.message || 'Failed to save income source.';
		} finally {
			isSubmitting = false;
		}
	}

	async function handleDelete() {
		if (!editingIncome) return;
		isSubmitting = true;
		errorMsg = '';
		try {
			await deletePlannedIncome(editingIncome.id);
			onSuccess();
		} catch (err: any) {
			errorMsg = err?.message || 'Failed to delete income source.';
		} finally {
			isSubmitting = false;
		}
	}

	function handleKeydown(e: KeyboardEvent) {
		if (isOpen && e.key === 'Escape' && !isSubmitting) {
			e.preventDefault();
			onClose();
		}
	}
</script>

<svelte:window onkeydown={handleKeydown} />

{#if isOpen}
	<div class="fixed inset-0 z-50 flex items-center justify-center p-4">
		<button
			type="button"
			tabindex="-1"
			aria-label="Close income modal"
			onclick={() => {
				if (!isSubmitting) onClose();
			}}
			class="fixed inset-0 bg-black/50 backdrop-blur-xs"
		></button>

		<div
			role="dialog"
			aria-modal="true"
			aria-labelledby="income-modal-title"
			data-testid="income-modal"
			class="relative z-10 w-full max-w-md rounded-2xl border border-border bg-surface text-ink-primary p-6 shadow-2xl max-h-[90dvh] overflow-y-auto"
		>
			<div class="flex items-center justify-between border-b border-border pb-3">
				<h2 id="income-modal-title" class="text-base font-semibold text-ink-primary">
					{editingIncome ? 'Edit Income Source' : 'New Income Source'}
				</h2>
				<button
					type="button"
					aria-label="Close income modal"
					disabled={isSubmitting}
					onclick={onClose}
					class="rounded-lg p-1 text-ink-muted hover:bg-surface-hover hover:text-ink-primary disabled:opacity-40 transition-colors"
				>
					✕
				</button>
			</div>

			<form onsubmit={handleSubmit} class="mt-4 space-y-3.5">
				<div>
					<label for="income-name" class="block text-xs font-medium text-ink-secondary mb-1">
						Income Source Name
					</label>
					<!-- svelte-ignore a11y_autofocus -->
					<input
						id="income-name"
						type="text"
						placeholder="Paycheck / Income source"
						bind:value={name}
						autofocus
						required
						class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
					/>
				</div>

				<div>
					<label for="income-amount" class="block text-xs font-medium text-ink-secondary mb-1">
						Planned Monthly Amount ($)
					</label>
					<input
						id="income-amount"
						type="number"
						step="0.01"
						min="0"
						placeholder="2500.00"
						bind:value={amount}
						required
						class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm font-mono text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
					/>
				</div>

				{#if errorMsg}
					<p
						role="alert"
						class="text-xs text-status-danger bg-status-danger-bg border border-status-danger/30 rounded-lg px-3 py-2"
					>
						{errorMsg}
					</p>
				{/if}

				<div class="flex items-center justify-between pt-3">
					{#if editingIncome}
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
							disabled={isSubmitting}
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
							{isSubmitting ? 'Saving...' : editingIncome ? 'Save Income' : 'Add Income'}
						</button>
					</div>
				</div>
			</form>
		</div>
	</div>
{/if}
