<script lang="ts">
	import { reallocateFunds } from '$lib/api/budget';
	import CategorySearchPicker from './CategorySearchPicker.svelte';

	export interface DonorCategory {
		id: string;
		name: string;
		available: number;
	}

	interface Props {
		isOpen: boolean;
		yearMonth?: string;
		overspentCategory: { id: string; name: string; remaining: number } | null;
		availableDonors: DonorCategory[];
		onClose: () => void;
		onSuccess: () => void;
	}

	let {
		isOpen,
		yearMonth = new Date().toISOString().slice(0, 7) + '-01',
		overspentCategory,
		availableDonors,
		onClose,
		onSuccess
	}: Props = $props();

	let selectableDonors = $derived(availableDonors.filter((d) => d.available > 0));
	let selectedDonorId = $state('');
	let amountInput = $state('');
	let reasonInput = $state('');
	let isSubmitting = $state(false);
	let errorMsg = $state('');

	$effect(() => {
		if (isOpen && overspentCategory) {
			const deficit = Math.abs(overspentCategory.remaining);
			amountInput = deficit > 0 ? deficit.toFixed(2) : '';
			selectedDonorId = selectableDonors[0]?.id ?? '';
			reasonInput = '';
			errorMsg = '';
		}
	});

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		if (!overspentCategory || !selectedDonorId) return;

		const parsedAmount = Number(amountInput);
		if (!Number.isFinite(parsedAmount) || parsedAmount <= 0) {
			errorMsg = 'Please enter an amount greater than $0.00.';
			return;
		}

		isSubmitting = true;
		errorMsg = '';
		try {
			await reallocateFunds({
				year_month: yearMonth,
				from_category_id: selectedDonorId,
				to_category_id: overspentCategory.id,
				amount: parsedAmount,
				reason: reasonInput.trim() || `Cover ${overspentCategory.name} overspend`
			});
			onSuccess();
		} catch (err: any) {
			errorMsg = err?.message || 'Failed to transfer funds.';
		} finally {
			isSubmitting = false;
		}
	}
</script>

{#if isOpen && overspentCategory}
	<div class="fixed inset-0 z-50 flex items-end sm:items-stretch sm:justify-end">
		<button
			type="button"
			aria-label="Close cover drawer"
			onclick={onClose}
			class="fixed inset-0 bg-black/50 backdrop-blur-xs transition-opacity"
		></button>

		<div
			role="dialog"
			aria-modal="true"
			aria-labelledby="cover-drawer-title"
			class="relative z-10 w-full sm:max-w-md bg-surface text-ink-primary border-t sm:border-t-0 sm:border-l border-border rounded-t-2xl sm:rounded-none sm:rounded-l-2xl p-6 shadow-2xl flex flex-col justify-between max-h-[90dvh] sm:max-h-full overflow-y-auto"
		>
			<div>
				<div class="flex items-center justify-between border-b border-border pb-4">
					<div>
						<h2 id="cover-drawer-title" class="text-lg font-semibold text-ink-primary">
							Cover Overspending
						</h2>
						<p class="text-xs text-ink-secondary mt-0.5">
							Move funds into <span class="text-status-danger font-medium">{overspentCategory.name}</span>
						</p>
					</div>
					<button
						type="button"
						onclick={onClose}
						class="rounded-lg p-1.5 text-ink-muted hover:bg-surface-hover hover:text-ink-primary transition-colors"
					>
						✕
					</button>
				</div>

				<form onsubmit={handleSubmit} class="mt-5 space-y-4">
					<div>
						<label for="donor-select" class="block text-xs font-medium text-ink-secondary mb-1.5">
							Move Money From
						</label>
						{#if selectableDonors.length === 0}
							<p class="rounded-lg border border-status-warn/30 bg-status-warn-bg p-3 text-xs text-status-warn">
								No categories currently have a positive available balance to donate from.
							</p>
						{:else}
							<CategorySearchPicker
								id="donor-select"
								categories={selectableDonors.map((d) => ({
									id: d.id,
									name: d.name,
									subtitle: `$${d.available.toFixed(2)} available`
								}))}
								bind:value={selectedDonorId}
								ariaLabel="Move Money From"
								collapsible={true}
								testIdPrefix="cover-donor"
							/>
						{/if}
					</div>

					<div>
						<label for="cover-amount" class="block text-xs font-medium text-ink-secondary mb-1.5">
							Amount ($)
						</label>
						<input
							id="cover-amount"
							type="number"
							step="0.01"
							min="0.01"
							bind:value={amountInput}
							required
							class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
						/>
					</div>

					<div>
						<label for="cover-reason" class="block text-xs font-medium text-ink-secondary mb-1.5">
							Reason (optional)
						</label>
						<input
							id="cover-reason"
							type="text"
							placeholder="e.g., Extra groceries for guests"
							bind:value={reasonInput}
							class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
						/>
					</div>

					{#if errorMsg}
						<p class="text-xs text-status-danger bg-status-danger-bg border border-status-danger/30 rounded-lg px-3 py-2">
							{errorMsg}
						</p>
					{/if}

					<div class="flex items-center justify-end gap-3 pt-2">
						<button
							type="button"
							onclick={onClose}
							class="rounded-lg border border-border bg-subtle px-4 py-2 text-xs font-medium text-ink-secondary hover:bg-surface-hover hover:text-ink-primary transition-colors"
						>
							Cancel
						</button>
						<button
							type="submit"
							disabled={isSubmitting || selectableDonors.length === 0}
							class="rounded-lg bg-accent-primary px-4 py-2 text-xs font-semibold text-accent-primary-text hover:opacity-90 disabled:opacity-50 transition-opacity"
						>
							{isSubmitting ? 'Transferring...' : 'Cover Overspend'}
						</button>
					</div>
				</form>
			</div>
		</div>
	</div>
{/if}
