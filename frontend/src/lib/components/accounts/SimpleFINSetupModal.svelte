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
	// The claim is a Temporal workflow now, so it can sit in RETRYING while the bank
	// catches up. Surfacing that beats a silent spinner the user assumes is broken.
	let progressMsg = $state<string | null>(null);

	function reset() {
		step = 1;
		setupToken = '';
		discovered = [];
		mappings = [];
		errorMsg = null;
		progressMsg = null;
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
			const accounts = await claimSimpleFINToken(setupToken.trim(), (status) => {
				if (status.status === 'RETRYING') {
					const attempt = status.attempt ?? 2;
					progressMsg = status.last_error
						? `Bank is slow or rate limited — retrying (attempt ${attempt}): ${status.last_error}`
						: `Bank is slow or rate limited — retrying (attempt ${attempt})...`;
				} else {
					progressMsg = 'Contacting your bank...';
				}
			});
			progressMsg = null;
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
			progressMsg = null;
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
	<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" role="dialog" aria-modal="true">
		<div class="w-full max-w-lg bg-surface border border-border text-ink-primary rounded-xl shadow-2xl p-5 space-y-4">
			<div class="flex items-center justify-between">
				<h3 class="text-base font-semibold text-ink-primary">
					{step === 1 ? 'Connect SimpleFIN Bridge' : 'Map Discovered Accounts'}
				</h3>
				<button
					type="button"
					onclick={handleClose}
					class="text-ink-muted hover:text-ink-primary hover:bg-surface-hover text-sm px-2 py-1 rounded-lg transition-colors"
				>
					✕
				</button>
			</div>

			{#if errorMsg}
				<div class="bg-status-danger-bg border border-status-danger/30 text-status-danger text-xs px-3 py-2 rounded-md">
					{errorMsg}
				</div>
			{/if}

			{#if step === 1}
				<form onsubmit={handleClaim} class="space-y-4">
					<div>
						<label for="sf-token" class="block text-xs text-ink-secondary mb-1.5">
							SimpleFIN Setup Token
						</label>
						<textarea
							id="sf-token"
							bind:value={setupToken}
							rows="3"
							placeholder="Paste your base64 SimpleFIN setup token..."
							class="w-full bg-canvas border border-border rounded-md px-3 py-2 text-xs font-mono text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
							required
						></textarea>
						<p class="text-[11px] text-ink-muted mt-1">
							Your access URL is encrypted with AES-256-GCM before storage.
						</p>
					</div>

					{#if progressMsg}
						<p class="text-[11px] text-ink-secondary flex items-center gap-1.5" role="status">
							<span class="inline-block w-1.5 h-1.5 rounded-full bg-accent-pop animate-pulse"></span>
							{progressMsg}
						</p>
					{/if}

					<div class="flex justify-end gap-2">
						<button
							type="button"
							onclick={handleClose}
							class="px-3 py-1.5 text-xs border border-border bg-subtle text-ink-secondary hover:bg-surface-hover hover:text-ink-primary rounded-md transition-colors"
						>
							Cancel
						</button>
						<button
							type="submit"
							disabled={loading || !setupToken.trim()}
							class="px-4 py-1.5 text-xs font-medium bg-accent-primary hover:opacity-90 disabled:opacity-50 text-accent-primary-text rounded-md transition-opacity"
						>
							{loading ? 'Claiming...' : 'Claim & Discover Accounts'}
						</button>
					</div>
				</form>
			{:else}
				<div class="space-y-3 max-h-80 overflow-y-auto">
					{#each mappings as mapping, idx (mapping.external_account_id)}
						<div class="bg-subtle border border-border rounded-lg p-3 space-y-2">
							<div class="flex items-center justify-between">
								<span class="text-sm font-medium text-ink-primary">{mapping.name}</span>
								<span class="text-xs font-mono text-ink-secondary">${discovered[idx]?.balance ?? '0.00'}</span>
							</div>

							<div class="grid grid-cols-2 gap-2">
								<div>
									<label class="block text-[11px] text-ink-secondary mb-1" for={`type-${idx}`}>
										Account Type
									</label>
									<select
										id={`type-${idx}`}
										bind:value={mapping.type}
										class="w-full bg-canvas border border-border rounded px-2 py-1 text-xs text-ink-primary focus:border-accent-pop focus:outline-none"
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
										<label class="block text-[11px] text-ink-secondary mb-1" for={`mode-${idx}`}>
											Card Mode
										</label>
										<select
											id={`mode-${idx}`}
											bind:value={mapping.card_mode}
											class="w-full bg-canvas border border-border rounded px-2 py-1 text-xs text-ink-primary focus:border-accent-pop focus:outline-none"
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
						class="px-3 py-1.5 text-xs border border-border bg-subtle text-ink-secondary hover:bg-surface-hover hover:text-ink-primary rounded-md transition-colors"
					>
						Back
					</button>
					<button
						type="button"
						onclick={handleConfirmLink}
						disabled={loading || mappings.length === 0}
						class="px-4 py-1.5 text-xs font-medium bg-accent-primary hover:opacity-90 disabled:opacity-50 text-accent-primary-text rounded-md transition-opacity"
					>
						{loading ? 'Linking...' : 'Confirm & Link Accounts'}
					</button>
				</div>
			{/if}
		</div>
	</div>
{/if}
