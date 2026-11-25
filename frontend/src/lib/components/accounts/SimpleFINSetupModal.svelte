<script lang="ts">
	import { onMount } from 'svelte';
	import {
		claimSimpleFINToken,
		linkSimpleFINAccounts,
		type LinkAccountMapping,
		type SimpleFINDiscoveredAccount
	} from '$lib/api/simplefin';
	import {
		SIMPLEFIN_BRIDGE_HOME_URL,
		SIMPLEFIN_CREATE_TOKEN_URL,
		SIMPLEFIN_MY_ACCOUNT_URL,
		formatSimpleFINClaimError,
		inferAccountType,
		validateSimpleFINSetupToken
	} from '$lib/simplefin/setup';
	import { demo } from '$lib/stores/demo.svelte';

	interface SelectableAccountMapping extends LinkAccountMapping {
		selected: boolean;
	}

	interface Props {
		open: boolean;
		onClose: () => void;
		onLinked: (linkedCount?: number) => void;
		isDemoMode?: boolean;
		isConnected?: boolean;
		onExitDemoMode?: () => Promise<void>;
		onTriggerSync?: () => Promise<unknown> | void;
	}

	let {
		open,
		onClose,
		onLinked,
		isDemoMode,
		isConnected,
		onExitDemoMode,
		onTriggerSync
	}: Props = $props();

	let dialogEl = $state<HTMLDialogElement | null>(null);
	let tokenInputEl = $state<HTMLTextAreaElement | null>(null);

	let step = $state<1 | 2>(1);
	let setupToken = $state('');
	let tokenTouched = $state(false);
	let submitAttempted = $state(false);
	let clipboardHint = $state<string | null>(null);
	let discovered = $state<SimpleFINDiscoveredAccount[]>([]);
	let mappings = $state<SelectableAccountMapping[]>([]);
	let claimWarnings = $state<string[]>([]);
	let loading = $state(false);
	let exitingDemo = $state(false);
	let errorMsg = $state<string | null>(null);
	// The claim is a Temporal workflow now, so it can sit in RETRYING while the bank
	// catches up. Surfacing that beats a silent spinner the user assumes is broken.
	let progressMsg = $state<string | null>(null);

	let isDemo = $derived(isDemoMode ?? demo.isDemo);
	let isReconnect = $derived(
		isConnected ?? (demo.simplefin.connected && !demo.simplefin.is_demo_bridge)
	);
	let isBusy = $derived(loading || exitingDemo);
	let canPaste = $derived(
		typeof navigator !== 'undefined' && typeof navigator.clipboard?.readText === 'function'
	);

	let validation = $derived(validateSimpleFINSetupToken(setupToken));
	let showValidationError = $derived((tokenTouched || submitAttempted) && !validation.valid);
	let selectedCount = $derived(mappings.filter((m) => m.selected).length);
	let allSelected = $derived(mappings.length > 0 && selectedCount === mappings.length);

	function reset() {
		step = 1;
		setupToken = '';
		tokenTouched = false;
		submitAttempted = false;
		clipboardHint = null;
		discovered = [];
		mappings = [];
		claimWarnings = [];
		errorMsg = null;
		progressMsg = null;
		loading = false;
		exitingDemo = false;
	}

	$effect(() => {
		if (!dialogEl) return;
		if (open) {
			if (!dialogEl.open) {
				dialogEl.showModal();
			}
		} else if (dialogEl.open) {
			dialogEl.close();
		}
	});

	// Fallback light-dismiss for browsers lacking native <dialog closedby="any">
	onMount(() => {
		if (!dialogEl || 'closedBy' in HTMLDialogElement.prototype) return;
		const el = dialogEl;
		let pressedOutside = false;
		const isOutside = (e: PointerEvent) => {
			const r = el.getBoundingClientRect();
			return e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom;
		};
		const onDown = (e: PointerEvent) => {
			pressedOutside = el.open && !isBusy && isOutside(e);
		};
		const onUp = (e: PointerEvent) => {
			if (pressedOutside && el.open && !isBusy && isOutside(e)) {
				el.close();
			}
			pressedOutside = false;
		};
		el.addEventListener('pointerdown', onDown);
		el.addEventListener('pointerup', onUp);
		return () => {
			el.removeEventListener('pointerdown', onDown);
			el.removeEventListener('pointerup', onUp);
		};
	});

	function handleNativeClose() {
		if (dialogEl) {
			dialogEl.returnValue = '';
		}
		reset();
		onClose();
	}

	function handleCancel(e: Event) {
		if (isBusy) {
			e.preventDefault();
		}
	}

	function requestClose() {
		if (isBusy) return;
		dialogEl?.close();
	}

	async function handleExitDemoMode() {
		exitingDemo = true;
		errorMsg = null;
		try {
			await (onExitDemoMode ? onExitDemoMode() : demo.toggleDemo(false));
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to exit Demo Mode.';
		} finally {
			exitingDemo = false;
		}
	}

	async function handlePasteFromClipboard() {
		clipboardHint = null;
		errorMsg = null;
		try {
			const text = await navigator.clipboard.readText();
			if (!text.trim()) {
				clipboardHint =
					'Your clipboard is empty. Copy the Setup Token from SimpleFIN Bridge first.';
				return;
			}
			setupToken = text;
			tokenTouched = true;
		} catch {
			clipboardHint =
				'Clipboard access was blocked. Click the box below and press Ctrl+V (or Cmd+V) to paste.';
			tokenInputEl?.focus();
		}
	}

	async function handleClaim(e: SubmitEvent) {
		e.preventDefault();
		submitAttempted = true;
		tokenTouched = true;
		errorMsg = null;
		clipboardHint = null;

		const result = validateSimpleFINSetupToken(setupToken);
		if (!result.valid) {
			tokenInputEl?.focus();
			return;
		}

		loading = true;
		claimWarnings = [];
		progressMsg = 'Contacting your bank...';
		try {
			let warningsFromWorkflow: string[] = [];
			const accounts = await claimSimpleFINToken(result.token, (status) => {
				if (status.status === 'RETRYING') {
					const attempt = status.attempt ?? 2;
					progressMsg = status.last_error
						? `Bank is slow or rate limited — retrying (attempt ${attempt}): ${status.last_error}`
						: `Bank is slow or rate limited — retrying (attempt ${attempt})...`;
				} else if (status.status === 'COMPLETED') {
					warningsFromWorkflow = status.result?.warnings ?? [];
				} else {
					progressMsg = 'Contacting your bank...';
				}
			});
			progressMsg = null;
			claimWarnings = warningsFromWorkflow;
			discovered = accounts;
			mappings = accounts.map((a) => ({
				external_account_id: a.id,
				name: a.name,
				type: inferAccountType(a.name),
				card_mode: 'PAY_IN_FULL',
				balance: a.balance,
				selected: true
			}));
			step = 2;
		} catch (err) {
			progressMsg = null;
			errorMsg = formatSimpleFINClaimError(
				err instanceof Error ? err.message : 'Failed to claim token'
			);
		} finally {
			loading = false;
		}
	}

	function toggleSelectAll() {
		const next = !allSelected;
		for (const item of mappings) {
			item.selected = next;
		}
	}

	async function handleConfirmLink() {
		const selectedMappings: LinkAccountMapping[] = mappings
			.filter((m) => m.selected)
			.map(({ selected: _selected, ...rest }) => rest);
		if (selectedMappings.length === 0) return;

		loading = true;
		errorMsg = null;
		try {
			await linkSimpleFINAccounts(selectedMappings);
			dialogEl?.close();
			onLinked(selectedMappings.length);
			void (onTriggerSync ? onTriggerSync() : demo.triggerSyncAndPoll());
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to link accounts';
		} finally {
			loading = false;
		}
	}

	function handleFinishEmptyDiscovery() {
		onLinked(0);
		dialogEl?.close();
	}
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<dialog
	bind:this={dialogEl}
	aria-labelledby="simplefin-modal-title"
	closedby="any"
	onclose={handleNativeClose}
	oncancel={handleCancel}
	class="m-auto w-full max-w-lg rounded-2xl border border-border bg-surface p-0 text-ink-primary shadow-2xl max-h-[90dvh] overflow-y-auto"
>
	<div class="p-5 space-y-4">
		<div class="flex items-center justify-between gap-3">
			<div class="flex items-center gap-2 min-w-0">
				<h1 id="simplefin-modal-title" class="text-base font-semibold text-ink-primary truncate">
					{#if isDemo}
						SimpleFIN Bank Sync
					{:else if step === 1}
						{isReconnect ? 'Reconnect SimpleFIN Bridge' : 'Connect SimpleFIN Bridge'}
					{:else}
						Map Discovered Accounts
					{/if}
				</h1>
				{#if !isDemo}
					<span class="shrink-0 text-[11px] font-medium px-2 py-0.5 rounded-full bg-subtle border border-border text-ink-secondary">
						Step {step} of 2
					</span>
				{/if}
			</div>
			<button
				type="button"
				onclick={requestClose}
				disabled={isBusy}
				aria-label="Close SimpleFIN modal"
				class="text-ink-muted hover:text-ink-primary hover:bg-surface-hover disabled:opacity-40 text-sm px-2 py-1 rounded-lg transition-colors"
			>
				✕
			</button>
		</div>

		{#if errorMsg}
			<div
				role="alert"
				class="bg-status-danger-bg border border-status-danger/30 text-status-danger text-xs px-3 py-2 rounded-md"
			>
				{errorMsg}
			</div>
		{/if}

		{#if isDemo}
			<div
				data-testid="simplefin-demo-blocked"
				class="rounded-xl border border-accent-pop/30 bg-accent-tint p-4 space-y-2.5"
			>
				<p class="text-xs font-semibold text-ink-primary">
					Demo Mode uses the built-in SimpleFIN simulator
				</p>
				<p class="text-xs text-ink-secondary leading-relaxed">
					Connecting a real SimpleFIN Bridge token is disabled while you are in Demo Mode so real
					bank credentials stay isolated in your real workspace.
				</p>
			</div>

			<div class="flex justify-end gap-2 pt-1">
				<button
					type="button"
					onclick={requestClose}
					disabled={isBusy}
					class="px-3 py-1.5 text-xs border border-border bg-subtle text-ink-secondary hover:bg-surface-hover hover:text-ink-primary rounded-md transition-colors"
				>
					Close
				</button>
				<button
					type="button"
					onclick={() => void handleExitDemoMode()}
					disabled={isBusy}
					data-testid="exit-demo-mode-btn"
					class="px-4 py-1.5 text-xs font-medium bg-accent-primary hover:opacity-90 disabled:opacity-50 text-accent-primary-text rounded-md transition-opacity"
				>
					{exitingDemo ? 'Switching...' : 'Exit Demo Mode'}
				</button>
			</div>
		{:else if step === 1}
			<p class="text-xs text-ink-secondary leading-relaxed">
				SimpleFIN Bridge is a separate, read-only bank sync service (paid directly to SimpleFIN)
				that connects your banks without sharing login passwords with Budget App.
			</p>

			<details
				open={!isReconnect}
				data-testid="simplefin-guide"
				class="rounded-xl border border-border bg-subtle/70 p-3.5 space-y-2.5"
			>
				<summary class="cursor-pointer select-none text-xs font-semibold text-ink-primary">
					{isReconnect
						? 'Need a new setup token? View setup steps'
						: 'How to get your SimpleFIN Setup Token'}
				</summary>

				<ol class="list-decimal list-inside space-y-1.5 text-xs text-ink-secondary pt-1">
					<li>
						<strong class="text-ink-primary">Open SimpleFIN Bridge</strong> and sign in — or create
						an account and link your banks under <strong class="text-ink-primary">Financial Institutions</strong>.
					</li>
					<li>
						Go to <strong class="text-ink-primary">Apps → New Connection</strong>, give it a name
						(like <span class="italic">Budget App</span>), and click
						<strong class="text-ink-primary">Create Setup Token</strong>.
					</li>
					<li>
						<strong class="text-ink-primary">Copy the single-use Setup Token</strong> and paste it
						below.
					</li>
				</ol>

				<div class="flex flex-wrap items-center gap-2 pt-1">
					<a
						href={SIMPLEFIN_CREATE_TOKEN_URL}
						target="_blank"
						rel="noopener noreferrer"
						data-testid="open-simplefin-create"
						class="inline-flex items-center gap-1 px-3 py-1.5 rounded-md text-xs font-medium bg-accent-primary hover:opacity-90 text-accent-primary-text transition-opacity"
					>
						Create Setup Token on SimpleFIN ↗
					</a>
					<a
						href={SIMPLEFIN_BRIDGE_HOME_URL}
						target="_blank"
						rel="noopener noreferrer"
						data-testid="open-simplefin-home"
						class="text-xs font-medium text-accent-pop hover:underline px-1 py-1"
					>
						New to SimpleFIN? Sign up &amp; link banks ↗
					</a>
				</div>
			</details>

			{#if isReconnect}
				<div class="flex items-center justify-between gap-2 text-xs text-ink-secondary bg-subtle/50 border border-border rounded-lg px-3 py-2">
					<span>Need a fresh single-use token?</span>
					<a
						href={SIMPLEFIN_CREATE_TOKEN_URL}
						target="_blank"
						rel="noopener noreferrer"
						data-testid="reconnect-simplefin-create"
						class="font-medium text-accent-pop hover:underline shrink-0"
					>
						Create new token on SimpleFIN ↗
					</a>
				</div>
			{/if}

			<form onsubmit={handleClaim} novalidate class="space-y-3">
				<div>
					<div class="flex items-center justify-between gap-2 mb-1">
						<label for="sf-token" class="block text-xs font-medium text-ink-primary">
							SimpleFIN Setup Token
						</label>
						{#if canPaste}
							<button
								type="button"
								onclick={() => void handlePasteFromClipboard()}
								disabled={isBusy}
								data-testid="paste-token-btn"
								class="px-2 py-0.5 text-[11px] font-medium rounded border border-border bg-subtle hover:bg-surface-hover text-ink-secondary hover:text-ink-primary transition-colors"
							>
								Paste
							</button>
						{/if}
					</div>
					<p id="sf-token-hint" class="text-[11px] text-ink-muted mb-1.5">
						Single-use base64 token from SimpleFIN Bridge. Your access URL is encrypted with
						AES-256-GCM before storage.
					</p>
					<!-- svelte-ignore a11y_autofocus -->
					<textarea
						id="sf-token"
						bind:this={tokenInputEl}
						bind:value={setupToken}
						autofocus
						rows="3"
						spellcheck="false"
						autocomplete="off"
						aria-invalid={showValidationError ? 'true' : 'false'}
						aria-describedby={showValidationError
							? 'sf-token-hint sf-token-error'
							: 'sf-token-hint'}
						onblur={() => {
							if (setupToken.trim()) tokenTouched = true;
						}}
						onpaste={() => {
							tokenTouched = true;
							clipboardHint = null;
						}}
						oninput={() => {
							errorMsg = null;
							clipboardHint = null;
						}}
						placeholder="Paste your base64 SimpleFIN setup token..."
						class="w-full bg-canvas border rounded-md px-3 py-2 text-xs font-mono text-ink-primary placeholder:text-ink-muted focus:outline-none {showValidationError
							? 'border-status-danger focus:border-status-danger'
							: validation.valid
								? 'border-status-good focus:border-status-good'
								: 'border-border focus:border-accent-pop'}"
					></textarea>

					{#if showValidationError && !validation.valid}
						<p
							id="sf-token-error"
							data-testid="sf-token-error"
							class="text-[11px] text-status-danger mt-1"
						>
							{validation.message}
						</p>
					{:else if validation.valid}
						<p data-testid="token-valid-indicator" class="text-[11px] text-status-good mt-1">
							Valid setup token format — ready to claim.
						</p>
					{/if}

					{#if clipboardHint}
						<p class="text-[11px] text-ink-secondary mt-1">{clipboardHint}</p>
					{/if}
				</div>

				{#if progressMsg}
					<p class="text-[11px] text-ink-secondary flex items-center gap-1.5" role="status">
						<span class="inline-block w-1.5 h-1.5 rounded-full bg-accent-pop animate-pulse"></span>
						{progressMsg}
					</p>
				{/if}

				<div class="flex justify-end gap-2 pt-1">
					<button
						type="button"
						onclick={requestClose}
						disabled={isBusy}
						class="px-3 py-1.5 text-xs border border-border bg-subtle text-ink-secondary hover:bg-surface-hover hover:text-ink-primary rounded-md transition-colors"
					>
						Cancel
					</button>
					<button
						type="submit"
						disabled={loading || !setupToken.trim()}
						data-testid="claim-token-btn"
						class="px-4 py-1.5 text-xs font-medium bg-accent-primary hover:opacity-90 disabled:opacity-50 text-accent-primary-text rounded-md transition-opacity"
					>
						{loading ? 'Claiming...' : 'Claim & Discover Accounts'}
					</button>
				</div>
			</form>
		{:else}
			{#if claimWarnings.length > 0}
				<div class="rounded-lg border border-status-warn/30 bg-status-warn-bg p-3 text-xs text-status-warn space-y-1">
					<p class="font-semibold">SimpleFIN reported warnings</p>
					<ul class="list-disc list-inside space-y-0.5">
						{#each claimWarnings as warning, idx (idx)}
							<li>{warning}</li>
						{/each}
					</ul>
				</div>
			{/if}

			{#if discovered.length === 0}
				<div
					data-testid="simplefin-no-accounts"
					class="rounded-xl border border-border bg-subtle p-4 space-y-2.5"
				>
					<p class="text-xs font-semibold text-ink-primary">
						Connected, but no bank accounts were found
					</p>
					<p class="text-xs text-ink-secondary leading-relaxed">
						Your SimpleFIN Bridge connection succeeded, but SimpleFIN didn't return any accounts
						yet. Link your bank under <strong class="text-ink-primary">Financial Institutions</strong>
						on SimpleFIN Bridge, then reconnect with a new setup token to import those accounts.
					</p>
					<div class="pt-1">
						<a
							href={SIMPLEFIN_MY_ACCOUNT_URL}
							target="_blank"
							rel="noopener noreferrer"
							data-testid="open-simplefin-accounts"
							class="inline-flex items-center gap-1 px-3 py-1.5 rounded-md text-xs font-medium bg-accent-primary hover:opacity-90 text-accent-primary-text transition-opacity"
						>
							Open SimpleFIN Accounts ↗
						</a>
					</div>
				</div>

				<div class="flex justify-end gap-2 pt-1">
					<button
						type="button"
						onclick={handleFinishEmptyDiscovery}
						class="px-4 py-1.5 text-xs font-medium bg-accent-primary hover:opacity-90 text-accent-primary-text rounded-md transition-opacity"
					>
						Close
					</button>
				</div>
			{:else}
				<div class="flex items-center justify-between text-xs text-ink-secondary">
					<span>{selectedCount} of {mappings.length} accounts selected</span>
					{#if mappings.length > 1}
						<button
							type="button"
							onclick={toggleSelectAll}
							data-testid="toggle-select-all"
							class="font-medium text-accent-pop hover:underline"
						>
							{allSelected ? 'Deselect all' : 'Select all'}
						</button>
					{/if}
				</div>

				<div class="space-y-3 max-h-80 overflow-y-auto pr-0.5">
					{#each mappings as mapping, idx (mapping.external_account_id)}
						<div
							class="bg-subtle border border-border rounded-lg p-3 space-y-2 transition-opacity {mapping.selected
								? ''
								: 'opacity-60'}"
						>
							<div class="flex items-center justify-between gap-2">
								<label class="flex items-center gap-2 min-w-0 cursor-pointer">
									<input
										type="checkbox"
										bind:checked={mapping.selected}
										aria-label={`Include ${mapping.name}`}
										class="rounded border-border bg-canvas accent-accent-pop"
									/>
									<span class="text-sm font-medium text-ink-primary truncate">{mapping.name}</span>
								</label>
								<span class="text-xs font-mono text-ink-secondary shrink-0">
									${discovered[idx]?.balance ?? '0.00'}
								</span>
							</div>

							<div class="grid grid-cols-2 gap-2">
								<div>
									<label class="block text-[11px] text-ink-secondary mb-1" for={`type-${idx}`}>
										Account Type
									</label>
									<select
										id={`type-${idx}`}
										bind:value={mapping.type}
										disabled={!mapping.selected}
										class="w-full bg-canvas border border-border rounded px-2 py-1 text-xs text-ink-primary focus:border-accent-pop focus:outline-none disabled:cursor-not-allowed"
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
											disabled={!mapping.selected}
											class="w-full bg-canvas border border-border rounded px-2 py-1 text-xs text-ink-primary focus:border-accent-pop focus:outline-none disabled:cursor-not-allowed"
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
						onclick={() => {
							step = 1;
							errorMsg = null;
						}}
						disabled={loading}
						class="px-3 py-1.5 text-xs border border-border bg-subtle text-ink-secondary hover:bg-surface-hover hover:text-ink-primary rounded-md transition-colors"
					>
						Back
					</button>
					<button
						type="button"
						onclick={() => void handleConfirmLink()}
						disabled={loading || selectedCount === 0}
						data-testid="confirm-link-btn"
						class="px-4 py-1.5 text-xs font-medium bg-accent-primary hover:opacity-90 disabled:opacity-50 text-accent-primary-text rounded-md transition-opacity"
					>
						{#if loading}
							Linking...
						{:else}
							Link {selectedCount} {selectedCount === 1 ? 'Account' : 'Accounts'}
						{/if}
					</button>
				</div>
			{/if}
		{/if}
	</div>
</dialog>

<style>
	dialog:modal {
		overscroll-behavior: contain;
	}
	dialog::backdrop {
		overflow: hidden;
		overscroll-behavior: contain;
		background: rgb(0 0 0 / 0.65);
		backdrop-filter: blur(4px);
	}
</style>
