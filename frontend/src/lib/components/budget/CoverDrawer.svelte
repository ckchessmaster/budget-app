<script lang="ts">
	import { reallocateFunds } from '$lib/api/budget';

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
			class="fixed inset-0 bg-black/50 dark:bg-black/60 backdrop-blur-xs transition-opacity"
		></button>

		<div
			role="dialog"
			aria-modal="true"
			aria-labelledby="cover-drawer-title"
			class="relative z-10 w-full sm:max-w-md bg-white dark:bg-slate-900 border-t sm:border-t-0 sm:border-l border-slate-200 dark:border-slate-800 rounded-t-2xl sm:rounded-none sm:rounded-l-2xl p-6 shadow-2xl flex flex-col justify-between max-h-[90dvh] sm:max-h-full overflow-y-auto"
		>
			<div>
				<div class="flex items-center justify-between border-b border-slate-200 dark:border-slate-800 pb-4">
					<div>
						<h2 id="cover-drawer-title" class="text-lg font-semibold text-slate-900 dark:text-white">
							Cover Overspending
						</h2>
						<p class="text-xs text-slate-500 dark:text-slate-400 mt-0.5">
							Move funds into <span class="text-rose-600 dark:text-rose-400 font-medium">{overspentCategory.name}</span>
						</p>
					</div>
					<button
						type="button"
						onclick={onClose}
						class="rounded-lg p-1.5 text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800 hover:text-slate-700 dark:hover:text-white transition-colors"
					>
						✕
					</button>
				</div>

				<form onsubmit={handleSubmit} class="mt-5 space-y-4">
					<div>
						<label for="donor-select" class="block text-xs font-medium text-slate-700 dark:text-slate-300 mb-1.5">
							Move Money From
						</label>
						{#if selectableDonors.length === 0}
							<p class="rounded-lg border border-amber-300 dark:border-amber-500/30 bg-amber-50 dark:bg-amber-500/10 p-3 text-xs text-amber-800 dark:text-amber-300">
								No categories currently have a positive available balance to donate from.
							</p>
						{:else}
							<select
								id="donor-select"
								bind:value={selectedDonorId}
								class="w-full rounded-lg border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-3 py-2 text-sm text-slate-900 dark:text-white focus:border-emerald-500 focus:outline-none"
							>
								{#each selectableDonors as donor}
									<option value={donor.id}>
										{donor.name} (${donor.available.toFixed(2)} available)
									</option>
								{/each}
							</select>
						{/if}
					</div>

					<div>
						<label for="cover-amount" class="block text-xs font-medium text-slate-700 dark:text-slate-300 mb-1.5">
							Amount ($)
						</label>
						<input
							id="cover-amount"
							type="number"
							step="0.01"
							min="0.01"
							bind:value={amountInput}
							required
							class="w-full rounded-lg border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-3 py-2 text-sm text-slate-900 dark:text-white focus:border-emerald-500 focus:outline-none"
						/>
					</div>

					<div>
						<label for="cover-reason" class="block text-xs font-medium text-slate-700 dark:text-slate-300 mb-1.5">
							Reason (optional)
						</label>
						<input
							id="cover-reason"
							type="text"
							placeholder="e.g., Extra groceries for guests"
							bind:value={reasonInput}
							class="w-full rounded-lg border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-3 py-2 text-sm text-slate-900 dark:text-white focus:border-emerald-500 focus:outline-none"
						/>
					</div>

					{#if errorMsg}
						<p class="text-xs text-rose-700 dark:text-rose-400 bg-rose-50 dark:bg-rose-500/10 border border-rose-200 dark:border-rose-500/30 rounded-lg px-3 py-2">
							{errorMsg}
						</p>
					{/if}

					<div class="flex items-center justify-end gap-3 pt-2">
						<button
							type="button"
							onclick={onClose}
							class="rounded-lg border border-slate-300 dark:border-slate-700 px-4 py-2 text-xs font-medium text-slate-700 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
						>
							Cancel
						</button>
						<button
							type="submit"
							disabled={isSubmitting || selectableDonors.length === 0}
							class="rounded-lg bg-emerald-600 px-4 py-2 text-xs font-semibold text-white hover:bg-emerald-500 disabled:opacity-50 transition-colors"
						>
							{isSubmitting ? 'Transferring...' : 'Cover Overspend'}
						</button>
					</div>
				</form>
			</div>
		</div>
	</div>
{/if}
