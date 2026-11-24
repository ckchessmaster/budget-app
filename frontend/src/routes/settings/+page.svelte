<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { disconnectSimpleFIN } from '$lib/api/simplefin';
	import SimpleFINSetupModal from '$lib/components/accounts/SimpleFINSetupModal.svelte';
	import SimulatorWorkbench from '$lib/components/demo/SimulatorWorkbench.svelte';
	import { auth } from '$lib/stores/auth.svelte';
	import { demo } from '$lib/stores/demo.svelte';
	import { theme } from '$lib/stores/theme.svelte';

	let workspaceNameInput = $state('');
	let savingWorkspace = $state(false);
	let workspaceFeedback = $state<string | null>(null);
	let workspaceError = $state<string | null>(null);

	let togglingDemo = $state(false);
	let resettingDemo = $state(false);
	let confirmResetDemo = $state(false);
	let demoError = $state<string | null>(null);

	let simplefinModalOpen = $state(false);
	let confirmDisconnect = $state(false);
	let disconnecting = $state(false);
	let bankError = $state<string | null>(null);

	let copiedAlias = $state(false);

	let themeMode = $derived<'light' | 'dark' | 'system'>(theme.override ?? 'system');
	let isSyncing = $derived(demo.syncStatus === 'running' || demo.syncStatus === 'retrying');

	$effect(() => {
		if (demo.loaded) {
			workspaceNameInput = demo.workspaceName;
		}
	});

	onMount(async () => {
		await auth.ready();
		if (!auth.isAuthenticated) {
			void goto('/login');
			return;
		}
		await demo.load().catch((err) => {
			demoError = err instanceof Error ? err.message : 'Failed to load settings';
		});
	});

	async function handleToggleDemo(e: Event) {
		const target = e.currentTarget as HTMLInputElement;
		const nextEnabled = target.checked;
		togglingDemo = true;
		demoError = null;
		confirmResetDemo = false;
		try {
			await demo.toggleDemo(nextEnabled);
		} catch (err) {
			demoError = err instanceof Error ? err.message : 'Failed to toggle demo mode';
			target.checked = demo.isDemo;
		} finally {
			togglingDemo = false;
		}
	}

	async function handleResetDemo() {
		resettingDemo = true;
		demoError = null;
		try {
			await demo.resetDemo();
			confirmResetDemo = false;
		} catch (err) {
			demoError = err instanceof Error ? err.message : 'Failed to reset demo data';
		} finally {
			resettingDemo = false;
		}
	}

	async function handleSaveWorkspace(e: SubmitEvent) {
		e.preventDefault();
		if (!workspaceNameInput.trim()) return;
		savingWorkspace = true;
		workspaceError = null;
		workspaceFeedback = null;
		try {
			await demo.updateWorkspaceName(workspaceNameInput.trim());
			workspaceFeedback = 'Workspace name updated.';
		} catch (err) {
			workspaceError = err instanceof Error ? err.message : 'Failed to update workspace';
		} finally {
			savingWorkspace = false;
		}
	}

	async function handleDisconnect() {
		disconnecting = true;
		bankError = null;
		try {
			await disconnectSimpleFIN(false);
			confirmDisconnect = false;
			await demo.load();
		} catch (err) {
			bankError = err instanceof Error ? err.message : 'Failed to disconnect SimpleFIN';
		} finally {
			disconnecting = false;
		}
	}

	async function handleCopyAlias() {
		if (!demo.inboundReceiptAlias || typeof navigator === 'undefined' || !navigator.clipboard) {
			return;
		}
		await navigator.clipboard.writeText(demo.inboundReceiptAlias);
		copiedAlias = true;
		setTimeout(() => {
			copiedAlias = false;
		}, 2000);
	}

	function formatTimestamp(iso: string | null): string {
		if (!iso) return 'Never';
		const d = new Date(iso);
		if (Number.isNaN(d.getTime())) return iso;
		return d.toLocaleString();
	}
</script>

<div class="max-w-5xl mx-auto p-4 sm:p-6 space-y-6">
	<div class="flex flex-wrap items-center justify-between gap-4">
		<div>
			<h2 class="text-xl font-bold text-slate-900 dark:text-slate-100">Settings</h2>
			<p class="text-xs text-slate-500 dark:text-slate-400">
				Demo Sandbox &amp; Fake SimpleFIN Simulator, Bank Connection, Workspace, and Appearance
			</p>
		</div>
	</div>

	<div class="grid grid-cols-1 lg:grid-cols-[200px_minmax(0,1fr)] gap-6 items-start">
		<!-- Section Navigation -->
		<nav
			aria-label="Settings sections"
			class="flex lg:flex-col gap-1 overflow-x-auto lg:sticky lg:top-6 pb-1 lg:pb-0"
		>
			<a
				href="#demo"
				class="px-3 py-2 rounded-lg text-xs font-semibold text-slate-700 dark:text-slate-200 hover:bg-white dark:hover:bg-slate-900 border border-transparent hover:border-slate-200 dark:hover:border-slate-800 whitespace-nowrap"
			>
				Demo Mode &amp; Simulator
			</a>
			<a
				href="#bank"
				class="px-3 py-2 rounded-lg text-xs font-semibold text-slate-700 dark:text-slate-200 hover:bg-white dark:hover:bg-slate-900 border border-transparent hover:border-slate-200 dark:hover:border-slate-800 whitespace-nowrap"
			>
				Bank Connection
			</a>
			<a
				href="#workspace"
				class="px-3 py-2 rounded-lg text-xs font-semibold text-slate-700 dark:text-slate-200 hover:bg-white dark:hover:bg-slate-900 border border-transparent hover:border-slate-200 dark:hover:border-slate-800 whitespace-nowrap"
			>
				Workspace
			</a>
			<a
				href="#appearance"
				class="px-3 py-2 rounded-lg text-xs font-semibold text-slate-700 dark:text-slate-200 hover:bg-white dark:hover:bg-slate-900 border border-transparent hover:border-slate-200 dark:hover:border-slate-800 whitespace-nowrap"
			>
				Appearance
			</a>
		</nav>

		<!-- Sections -->
		<div class="space-y-6 min-w-0">
			<!-- Section 1: Demo Mode & Fake SimpleFIN Simulator -->
			<section
				id="demo"
				class="rounded-2xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 p-5 space-y-5 shadow-xs"
			>
				<div class="flex flex-wrap items-start justify-between gap-4">
					<div class="space-y-1 max-w-xl">
						<div class="flex items-center gap-2">
							<h3 class="text-base font-bold text-slate-900 dark:text-white">
								Demo Mode &amp; SimpleFIN Sandbox
							</h3>
							{#if demo.isDemo}
								<span class="px-2 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-wide bg-amber-100 text-amber-800 dark:bg-amber-500/20 dark:text-amber-300">
									Active
								</span>
							{/if}
						</div>
						<p class="text-xs text-slate-500 dark:text-slate-400">
							Switch into a completely isolated demo workspace with a built-in fake SimpleFIN Bridge. Simulate pending authorizations, tip deltas, transfers, manual deduplication, and bank errors through the real Temporal pipeline without touching your real budget.
						</p>
					</div>

					<label class="inline-flex items-center gap-2.5 cursor-pointer select-none shrink-0">
						<span class="text-xs font-semibold text-slate-700 dark:text-slate-200">
							{demo.isDemo ? 'Demo Mode On' : 'Demo Mode Off'}
						</span>
						<input
							type="checkbox"
							{...({ switch: '' } as Record<string, unknown>)}
							checked={demo.isDemo}
							disabled={togglingDemo || resettingDemo}
							onchange={handleToggleDemo}
							data-testid="demo-mode-toggle"
							class="demo-switch bg-slate-300 dark:bg-slate-700 checked:bg-emerald-600"
						/>
					</label>
				</div>

				{#if demoError}
					<div class="rounded-lg border border-rose-200 dark:border-rose-500/30 bg-rose-50 dark:bg-rose-500/10 px-3 py-2 text-xs text-rose-700 dark:text-rose-300">
						{demoError}
					</div>
				{/if}

				{#if demo.isDemo}
					<div class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-amber-200 dark:border-amber-500/30 bg-amber-50/70 dark:bg-amber-500/10 px-4 py-3">
						<p class="text-xs text-amber-900 dark:text-amber-200">
							Toggling Demo Mode off preserves this sandbox for next time and returns you to your personal workspace.
						</p>

						<div class="flex items-center gap-2 shrink-0">
							{#if confirmResetDemo}
								<button
									type="button"
									disabled={resettingDemo}
									onclick={handleResetDemo}
									data-testid="confirm-reset-demo-btn"
									class="px-3 py-1.5 rounded-lg text-xs font-semibold bg-rose-600 hover:bg-rose-500 text-white"
								>
									{resettingDemo ? 'Resetting...' : 'Confirm Wipe & Re-seed'}
								</button>
								<button
									type="button"
									onclick={() => (confirmResetDemo = false)}
									class="px-3 py-1.5 rounded-lg text-xs text-slate-700 dark:text-slate-300 hover:bg-white/60 dark:hover:bg-slate-800"
								>
									Cancel
								</button>
							{:else}
								<button
									type="button"
									onclick={() => (confirmResetDemo = true)}
									data-testid="reset-demo-btn"
									class="px-3 py-1.5 rounded-lg text-xs font-semibold border border-rose-300 dark:border-rose-500/40 bg-white dark:bg-slate-900 text-rose-700 dark:text-rose-300 hover:bg-rose-50 dark:hover:bg-rose-500/15"
								>
									Reset Demo Data
								</button>
							{/if}
						</div>
					</div>

					<SimulatorWorkbench />
				{/if}
			</section>

			<!-- Section 2: Bank Connection (SimpleFIN) -->
			<section
				id="bank"
				class="rounded-2xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 p-5 space-y-4 shadow-xs"
			>
				<div class="flex flex-wrap items-start justify-between gap-3">
					<div>
						<h3 class="text-base font-bold text-slate-900 dark:text-white">
							Bank Connection (SimpleFIN)
						</h3>
						<p class="text-xs text-slate-500 dark:text-slate-400">
							Encrypted SimpleFIN Bridge connection for the active workspace
						</p>
					</div>

					<span
						class="px-2.5 py-1 rounded-full text-[11px] font-semibold {demo.simplefin.connected
							? demo.simplefin.is_demo_bridge
								? 'bg-sky-100 text-sky-800 dark:bg-sky-500/20 dark:text-sky-300'
								: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-500/20 dark:text-emerald-300'
							: 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-400'}"
					>
						{#if demo.simplefin.connected}
							{demo.simplefin.is_demo_bridge
								? 'Connected — Fake SimpleFIN Demo Bridge'
								: 'Connected — SimpleFIN Bridge'}
						{:else}
							Not Connected
						{/if}
					</span>
				</div>

				<div class="text-xs text-slate-600 dark:text-slate-300">
					<span class="text-slate-500 dark:text-slate-400">Last synced:</span>
					<span class="font-medium ml-1">{formatTimestamp(demo.simplefin.last_synced_at)}</span>
				</div>

				{#if demo.simplefin.last_sync_warnings.length > 0}
					<div class="rounded-lg border border-amber-200 dark:border-amber-500/30 bg-amber-50 dark:bg-amber-500/10 p-3 text-xs text-amber-800 dark:text-amber-200">
						<p class="font-semibold mb-1">Last Sync Warnings</p>
						<ul class="list-disc list-inside space-y-0.5">
							{#each demo.simplefin.last_sync_warnings as warn, idx (idx)}
								<li>{warn}</li>
							{/each}
						</ul>
					</div>
				{/if}

				{#if bankError}
					<div class="rounded-lg border border-rose-200 dark:border-rose-500/30 bg-rose-50 dark:bg-rose-500/10 px-3 py-2 text-xs text-rose-700 dark:text-rose-300">
						{bankError}
					</div>
				{/if}

				{#if demo.syncStatus !== 'idle'}
					<p class="text-xs text-slate-600 dark:text-slate-300">
						Sync status: <span class="font-medium">{demo.syncMessage}</span>
					</p>
				{/if}

				<div class="flex flex-wrap items-center gap-2 pt-1">
					<button
						type="button"
						disabled={isSyncing}
						onclick={() => void demo.triggerSyncAndPoll()}
						class="px-3 py-1.5 rounded-lg text-xs font-semibold bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 text-white"
					>
						{isSyncing ? 'Syncing...' : 'Sync Now'}
					</button>
					<button
						type="button"
						onclick={() => (simplefinModalOpen = true)}
						class="px-3 py-1.5 rounded-lg text-xs font-medium border border-slate-200 dark:border-slate-700 bg-slate-50 hover:bg-slate-100 dark:bg-slate-800 dark:hover:bg-slate-700 text-slate-800 dark:text-slate-200"
					>
						{demo.simplefin.connected ? 'Reconnect SimpleFIN' : 'Connect SimpleFIN'}
					</button>

					{#if demo.simplefin.connected}
						{#if confirmDisconnect}
							<button
								type="button"
								disabled={disconnecting}
								onclick={handleDisconnect}
								class="px-3 py-1.5 rounded-lg text-xs font-semibold bg-rose-600 hover:bg-rose-500 text-white"
							>
								{disconnecting ? 'Disconnecting...' : 'Confirm Disconnect'}
							</button>
							<button
								type="button"
								onclick={() => (confirmDisconnect = false)}
								class="px-3 py-1.5 rounded-lg text-xs text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200"
							>
								Cancel
							</button>
						{:else}
							<button
								type="button"
								onclick={() => (confirmDisconnect = true)}
								class="px-3 py-1.5 rounded-lg text-xs font-medium text-rose-600 dark:text-rose-400 hover:bg-rose-50 dark:hover:bg-rose-500/10"
							>
								Disconnect
							</button>
						{/if}
					{/if}
				</div>
			</section>

			<!-- Section 3: Workspace -->
			<section
				id="workspace"
				class="rounded-2xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 p-5 space-y-4 shadow-xs"
			>
				<div class="flex flex-wrap items-center justify-between gap-2">
					<div>
						<h3 class="text-base font-bold text-slate-900 dark:text-white">Workspace</h3>
						<p class="text-xs text-slate-500 dark:text-slate-400">
							Active workspace identity and inbound receipt forwarding alias
						</p>
					</div>
					<span
						class="px-2.5 py-0.5 rounded-full text-[11px] font-semibold {demo.isDemo
							? 'bg-amber-100 text-amber-800 dark:bg-amber-500/20 dark:text-amber-300'
							: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300'}"
					>
						{demo.isDemo ? 'Demo Sandbox' : 'Personal Workspace'}
					</span>
				</div>

				<form onsubmit={handleSaveWorkspace} class="flex flex-wrap items-end gap-2">
					<div class="flex-1 min-w-52">
						<label for="ws-name-input" class="block text-xs font-medium text-slate-700 dark:text-slate-300 mb-1">
							Workspace Name
						</label>
						<input
							id="ws-name-input"
							type="text"
							bind:value={workspaceNameInput}
							maxlength="255"
							required
							class="w-full rounded-lg border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-3 py-1.5 text-xs text-slate-900 dark:text-slate-100"
						/>
					</div>
					<button
						type="submit"
						disabled={savingWorkspace || !workspaceNameInput.trim()}
						class="px-3.5 py-1.5 rounded-lg text-xs font-semibold bg-slate-900 hover:bg-slate-800 dark:bg-slate-100 dark:hover:bg-white text-white dark:text-slate-900 disabled:opacity-50"
					>
						{savingWorkspace ? 'Saving...' : 'Save Name'}
					</button>
				</form>

				{#if workspaceFeedback}
					<p class="text-xs text-emerald-600 dark:text-emerald-400">{workspaceFeedback}</p>
				{/if}
				{#if workspaceError}
					<p class="text-xs text-rose-600 dark:text-rose-400">{workspaceError}</p>
				{/if}

				<div class="space-y-1.5 pt-1">
					<span class="block text-xs font-medium text-slate-700 dark:text-slate-300">
						Inbound Receipt Email Alias
					</span>
					<div class="flex flex-wrap items-center gap-2">
						<code class="px-2.5 py-1.5 rounded-lg border border-slate-200 dark:border-slate-800 bg-slate-50 dark:bg-slate-950 text-xs font-mono text-slate-800 dark:text-slate-200">
							{demo.inboundReceiptAlias || '—'}
						</code>
						<button
							type="button"
							onclick={handleCopyAlias}
							disabled={!demo.inboundReceiptAlias}
							class="px-2.5 py-1.5 rounded-lg text-xs font-medium border border-slate-200 dark:border-slate-700 hover:bg-slate-100 dark:hover:bg-slate-800 text-slate-700 dark:text-slate-200"
						>
							{copiedAlias ? 'Copied!' : 'Copy'}
						</button>
					</div>
				</div>

				{#if auth.user}
					<div class="pt-2 border-t border-slate-100 dark:border-slate-800 text-xs text-slate-500 dark:text-slate-400">
						Signed in as <span class="font-medium text-slate-800 dark:text-slate-200">{auth.user.email || auth.user.id}</span>
					</div>
				{/if}
			</section>

			<!-- Section 4: Appearance -->
			<section
				id="appearance"
				class="rounded-2xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 p-5 space-y-3 shadow-xs"
			>
				<div>
					<h3 class="text-base font-bold text-slate-900 dark:text-white">Appearance</h3>
					<p class="text-xs text-slate-500 dark:text-slate-400">
						Choose your color scheme or follow your operating system preference
					</p>
				</div>

				<div class="inline-flex rounded-xl border border-slate-200 dark:border-slate-800 bg-slate-50 dark:bg-slate-950 p-1 gap-1">
					{#each [
						{ id: 'light', label: 'Light' },
						{ id: 'dark', label: 'Dark' },
						{ id: 'system', label: 'System' }
					] as opt (opt.id)}
						<button
							type="button"
							onclick={() => theme.setMode(opt.id as 'light' | 'dark' | 'system')}
							aria-pressed={themeMode === opt.id}
							class="px-3.5 py-1.5 rounded-lg text-xs font-semibold transition-colors {themeMode === opt.id
								? 'bg-emerald-600 text-white shadow-xs'
								: 'text-slate-600 dark:text-slate-300 hover:bg-white dark:hover:bg-slate-800'}"
						>
							{opt.label}
						</button>
					{/each}
				</div>
			</section>
		</div>
	</div>
</div>

<SimpleFINSetupModal
	open={simplefinModalOpen}
	onClose={() => (simplefinModalOpen = false)}
	onLinked={() => void demo.load()}
/>

<style>
	.demo-switch {
		appearance: none;
		width: 2.75rem;
		height: 1.5rem;
		border-radius: 9999px;
		position: relative;
		cursor: pointer;
		accent-color: rgb(5 150 105);
		transition: background-color 150ms ease;
	}
	.demo-switch::after {
		content: '';
		position: absolute;
		top: 0.125rem;
		left: 0.125rem;
		width: 1.25rem;
		height: 1.25rem;
		border-radius: 9999px;
		background: white;
		box-shadow: 0 1px 2px rgb(0 0 0 / 0.25);
		transition: transform 150ms ease;
	}
	.demo-switch:checked::after {
		transform: translateX(1.25rem);
	}
	.demo-switch:focus-visible {
		outline: 3px solid rgb(5 150 105);
		outline-offset: 3px;
	}
	.demo-switch:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}
</style>
