<script lang="ts">
	import {
		claimSimpleFINToken,
		linkSimpleFINAccounts,
		type LinkAccountMapping,
		type SimpleFINDiscoveredAccount
	} from '$lib/api/simplefin';

	interface Props {
		open: boolean;
		onClose: () => void;
		onLinked: () => void;
	}

	let { open, onClose, onLinked }: Props = $props();

	let step = $state<1 | 2>(1);
	let setupToken = $state('');
	let discovered = $state<SimpleFINDiscoveredAccount[]>([]);
	let mappings = $state<LinkAccountMapping[]>([]);
	let loading = $state(false);
	let errorMsg = $state<string | null>(null);

	function reset() {
		step = 1;
		setupToken = '';
		discovered = [];
		mappings = [];
		errorMsg = null;
		loading = false;
	}

	function handleClose() {
		reset();
		onClose();
	}

	async function handleClaim(e: SubmitEvent) {
		e.preventDefault();
		if (!setupToken.trim()) return;
		loading = true;
		errorMsg = null;
		try {
			const accounts = await claimSimpleFINToken(setupToken.trim());
			discovered = accounts;
			mappings = accounts.map((a) => ({
				external_account_id: a.id,
				name: a.name,
				type: 'checking',
				card_mode: 'PAY_IN_FULL',
				balance: a.balance
			}));
			step = 2;
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to claim token';
		} finally {
			loading = false;
		}
	}

	async function handleConfirmLink() {
		loading = true;
		errorMsg = null;
		try {
			await linkSimpleFINAccounts(mappings);
			reset();
			onLinked();
			onClose();
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to link accounts';
		} finally {
			loading = false;
		}
	}
</script>

{#if open}
	<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4" role="dialog" aria-modal="true">
		<div class="w-full max-w-lg bg-slate-900 border border-slate-700 rounded-xl shadow-2xl p-5 space-y-4">
			<div class="flex items-center justify-between">
				<h3 class="text-base font-semibold text-slate-100">
					{step === 1 ? 'Connect SimpleFIN Bridge' : 'Map Discovered Accounts'}
				</h3>
				<button
					type="button"
					onclick={handleClose}
					class="text-slate-400 hover:text-slate-200 text-sm px-2 py-1 rounded"
				>
					✕
				</button>
			</div>

			{#if errorMsg}
				<div class="bg-rose-500/10 border border-rose-500/30 text-rose-300 text-xs px-3 py-2 rounded-md">
					{errorMsg}
				</div>
			{/if}

			{#if step === 1}
				<form onsubmit={handleClaim} class="space-y-4">
					<div>
						<label for="sf-token" class="block text-xs text-slate-300 mb-1.5">
							SimpleFIN Setup Token
						</label>
						<textarea
							id="sf-token"
							bind:value={setupToken}
							rows="3"
							placeholder="Paste your base64 SimpleFIN setup token..."
							class="w-full bg-slate-950 border border-slate-700 rounded-md px-3 py-2 text-xs font-mono text-slate-100"
							required
						></textarea>
						<p class="text-[11px] text-slate-400 mt-1">
							Your access URL is encrypted with AES-256-GCM before storage.
						</p>
					</div>

					<div class="flex justify-end gap-2">
						<button
							type="button"
							onclick={handleClose}
							class="px-3 py-1.5 text-xs text-slate-300 hover:bg-slate-800 rounded-md"
						>
							Cancel
						</button>
						<button
							type="submit"
							disabled={loading || !setupToken.trim()}
							class="px-4 py-1.5 text-xs font-medium bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 text-white rounded-md"
						>
							{loading ? 'Claiming...' : 'Claim & Discover Accounts'}
						</button>
					</div>
				</form>
			{:else}
				<div class="space-y-3 max-h-80 overflow-y-auto">
					{#each mappings as mapping, idx (mapping.external_account_id)}
						<div class="bg-slate-950 border border-slate-800 rounded-lg p-3 space-y-2">
							<div class="flex items-center justify-between">
								<span class="text-sm font-medium text-slate-100">{mapping.name}</span>
								<span class="text-xs font-mono text-slate-400">${discovered[idx]?.balance ?? '0.00'}</span>
							</div>

							<div class="grid grid-cols-2 gap-2">
								<div>
									<label class="block text-[11px] text-slate-400 mb-1" for={`type-${idx}`}>Account Type</label>
									<select
										id={`type-${idx}`}
										bind:value={mapping.type}
										class="w-full bg-slate-900 border border-slate-700 rounded px-2 py-1 text-xs text-slate-100"
									>
										<option value="checking">Checking</option>
										<option value="savings">Savings</option>
										<option value="cash">Cash</option>
										<option value="credit_card">Credit Card</option>
										<option value="investment">Investment</option>
										<option value="loan">Loan</option>
									</select>
								</div>

								{#if mapping.type === 'credit_card'}
									<div>
										<label class="block text-[11px] text-slate-400 mb-1" for={`mode-${idx}`}>Card Mode</label>
										<select
											id={`mode-${idx}`}
											bind:value={mapping.card_mode}
											class="w-full bg-slate-900 border border-slate-700 rounded px-2 py-1 text-xs text-slate-100"
										>
											<option value="PAY_IN_FULL">Pay In Full</option>
											<option value="DEBT_PAYDOWN">Debt Paydown</option>
										</select>
									</div>
								{/if}
							</div>
						</div>
					{/each}
				</div>

				<div class="flex justify-end gap-2 pt-2">
					<button
						type="button"
						onclick={() => (step = 1)}
						class="px-3 py-1.5 text-xs text-slate-300 hover:bg-slate-800 rounded-md"
					>
						Back
					</button>
					<button
						type="button"
						onclick={handleConfirmLink}
						disabled={loading || mappings.length === 0}
						class="px-4 py-1.5 text-xs font-medium bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 text-white rounded-md"
					>
						{loading ? 'Linking...' : 'Confirm & Link Accounts'}
					</button>
				</div>
			{/if}
		</div>
	</div>
{/if}
