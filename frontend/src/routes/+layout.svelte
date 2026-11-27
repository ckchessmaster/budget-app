<script lang="ts">
	import '../app.css';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import DemoSimulatorDrawer from '$lib/components/demo/DemoSimulatorDrawer.svelte';
	import PushPromptModal from '$lib/components/notifications/PushPromptModal.svelte';
	import InstallPromptBanner from '$lib/components/pwa/InstallPromptBanner.svelte';
	import {
		detectPushEnvironment,
		dismissPushPrompt,
		fetchPushConfig,
		getExistingPushSubscription,
		isPushPromptDismissed,
		shouldShowPushOnboardingModal,
		type PushEnvironment,
		type PushServerConfig
	} from '$lib/notifications/push';
	import { auth } from '$lib/stores/auth.svelte';
	import { demo } from '$lib/stores/demo.svelte';
	import { pwaInstall } from '$lib/stores/pwaInstall.svelte';
	import { theme } from '$lib/stores/theme.svelte';

	let { children } = $props();

	const PUBLIC_PATHS = ['/login', '/auth/callback', '/auth/silent-callback'];

	let currentPath = $derived(page.url.pathname);
	let isSilentCallbackRoute = $derived(currentPath.startsWith('/auth/silent-callback'));
	let isBudgetRoute = $derived(currentPath === '/budget' || currentPath.startsWith('/budget/'));
	let isBudgetActive = $derived(currentPath === '/' || currentPath.startsWith('/budget'));
	let isAccountsActive = $derived(currentPath.startsWith('/accounts'));
	let isRulesActive = $derived(currentPath.startsWith('/rules'));
	let isSettingsActive = $derived(currentPath.startsWith('/settings'));
	let isSyncing = $derived(demo.syncStatus === 'running' || demo.syncStatus === 'retrying');

	let pushModalOpen = $state(false);
	let pushConfig = $state<PushServerConfig | null>(null);
	let pushEnv = $state<PushEnvironment>(detectPushEnvironment());
	let pushChecked = $state(false);
	let isOffline = $state(typeof navigator !== 'undefined' ? !navigator.onLine : false);

	async function maybePromptPushNotifications() {
		const env = detectPushEnvironment();
		pushEnv = env;
		if (isPushPromptDismissed()) {
			return;
		}
		try {
			const [cfg, existing] = await Promise.all([
				fetchPushConfig().catch(() => null),
				getExistingPushSubscription().catch(() => null)
			]);
			if (!cfg) return;
			pushConfig = cfg;
			if (shouldShowPushOnboardingModal(env, cfg.enabled, Boolean(existing), isPushPromptDismissed())) {
				pushModalOpen = true;
			}
		} catch {
			// Non-fatal onboarding probe
		}
	}

	onMount(() => {
		if (page.url.pathname.startsWith('/auth/silent-callback')) {
			return;
		}
		theme.init();
		const stopPwaInstall = pwaInstall.init();

		const onOnline = () => {
			isOffline = false;
		};
		const onOffline = () => {
			isOffline = true;
		};
		window.addEventListener('online', onOnline);
		window.addEventListener('offline', onOffline);

		const cleanups: Array<() => void> = [
			stopPwaInstall,
			() => {
				window.removeEventListener('online', onOnline);
				window.removeEventListener('offline', onOffline);
			}
		];
		if (typeof navigator !== 'undefined' && 'serviceWorker' in navigator) {
			const hadController = Boolean(navigator.serviceWorker.controller);
			let reloading = false;

			const onControllerChange = () => {
				if (hadController && !reloading && navigator.onLine !== false) {
					reloading = true;
					window.location.reload();
				}
			};
			const onVisibilityChange = () => {
				if (document.visibilityState === 'visible' && navigator.onLine !== false) {
					void navigator.serviceWorker.getRegistration().then((reg) => reg?.update().catch(() => {}));
				}
			};

			navigator.serviceWorker.addEventListener('controllerchange', onControllerChange);
			document.addEventListener('visibilitychange', onVisibilityChange);
			cleanups.push(() => {
				navigator.serviceWorker.removeEventListener('controllerchange', onControllerChange);
				document.removeEventListener('visibilitychange', onVisibilityChange);
			});
		}

		return () => {
			for (const fn of cleanups) fn();
		};
	});

	$effect(() => {
		const path = page.url.pathname;
		const offlineNow = isOffline;
		if (PUBLIC_PATHS.some((p) => path.startsWith(p))) {
			return;
		}
		void (async () => {
			await auth.ready();
			if (!auth.isAuthenticated && !offlineNow && auth.hasCachedSession) {
				await auth.tryRestoreSession();
			}
			if (!auth.isAuthenticated && !auth.canAccessOffline) {
				pushChecked = false;
				pushModalOpen = false;
				demo.resetOnLogout();
				void goto('/login');
			} else if (auth.isAuthenticated) {
				if (!demo.loaded && !demo.loading) {
					void demo.load().catch(() => {});
				}
				if (!pushChecked) {
					pushChecked = true;
					void maybePromptPushNotifications();
				}
			}
		})();
	});
</script>

{#if isSilentCallbackRoute}
	{@render children()}
{:else}
<div class="flex flex-col h-dvh w-full bg-canvas text-ink-primary overflow-hidden font-sans">
	{#if !isBudgetRoute}
		<header
			style="padding-top: env(safe-area-inset-top, 0px);"
			class="flex flex-wrap items-center justify-between gap-2 sm:gap-4 px-3 sm:px-6 bg-surface border-b border-border shrink-0 select-none"
		>
			<div class="flex items-center gap-3 sm:gap-6">
				<a href="/budget" class="flex items-center gap-2.5 py-2.5 group">
					<div class="w-8 h-8 rounded-lg bg-accent-primary text-accent-primary-text flex items-center justify-center font-bold text-lg group-hover:opacity-90 transition-opacity">
						$
					</div>
					<div class="hidden sm:block">
						<h1 class="text-sm font-bold leading-none tracking-tight text-ink-primary">
							Budget App
						</h1>
						<p class="text-[11px] text-ink-secondary mt-0.5">Zero-Based Ledger</p>
					</div>
				</a>

				<nav aria-label="Primary" class="flex items-center gap-1">
					<a
						href="/budget"
						aria-current={isBudgetActive ? 'page' : undefined}
						class="relative px-3 py-3.5 text-xs font-semibold transition-colors {isBudgetActive
							? 'text-accent-pop after:content-[""] after:absolute after:inset-x-2 after:bottom-0 after:h-0.5 after:rounded-full after:bg-accent-pop'
							: 'text-ink-secondary hover:text-ink-primary'}"
					>
						Budget
					</a>
					<a
						href="/accounts"
						aria-current={isAccountsActive ? 'page' : undefined}
						class="relative px-3 py-3.5 text-xs font-semibold transition-colors {isAccountsActive
							? 'text-accent-pop after:content-[""] after:absolute after:inset-x-2 after:bottom-0 after:h-0.5 after:rounded-full after:bg-accent-pop'
							: 'text-ink-secondary hover:text-ink-primary'}"
					>
						Accounts
					</a>
					<a
						href="/rules"
						aria-current={isRulesActive ? 'page' : undefined}
						class="relative px-3 py-3.5 text-xs font-semibold transition-colors {isRulesActive
							? 'text-accent-pop after:content-[""] after:absolute after:inset-x-2 after:bottom-0 after:h-0.5 after:rounded-full after:bg-accent-pop'
							: 'text-ink-secondary hover:text-ink-primary'}"
					>
						Rules
					</a>
				</nav>
			</div>

			<div class="flex items-center gap-2 sm:gap-3">
				{#if auth.isAuthenticated}
					<a
						href="/settings"
						aria-label="Settings"
						title="Settings"
						aria-current={isSettingsActive ? 'page' : undefined}
						data-testid="settings-nav-link"
						class="p-2 rounded-lg border transition-colors {isSettingsActive
							? 'border-accent-pop/60 bg-accent-tint text-accent-pop'
							: 'border-border bg-subtle hover:bg-surface-hover text-ink-secondary hover:text-ink-primary'}"
					>
						<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
							<circle cx="12" cy="12" r="3" />
							<path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
						</svg>
					</a>
				{/if}

				<button
					type="button"
					onclick={() => theme.toggle()}
					aria-label={theme.effective === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
					title={theme.effective === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
					data-testid="theme-toggle-btn"
					class="p-2 rounded-lg border border-border bg-subtle hover:bg-surface-hover text-ink-secondary hover:text-ink-primary transition-colors"
				>
					{#if theme.effective === 'dark'}
						<!-- Sun icon -->
						<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
							<circle cx="12" cy="12" r="4" />
							<path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" />
						</svg>
					{:else}
						<!-- Moon icon -->
						<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
							<path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
						</svg>
					{/if}
				</button>

				{#if auth.isAuthenticated}
					<span class="hidden md:inline-block text-xs bg-subtle text-ink-secondary px-2.5 py-1 rounded-full border border-border font-mono">
						{auth.user?.email || auth.user?.id.slice(0, 8)}
					</span>
					<button
						type="button"
						onclick={() => {
							demo.resetOnLogout();
							void auth.logout();
						}}
						class="text-xs text-ink-secondary hover:text-ink-primary px-2.5 py-1.5 rounded-md hover:bg-surface-hover transition-colors"
					>
						Sign Out
					</button>
				{:else}
					<a
						href="/login"
						class="text-xs bg-accent-primary hover:opacity-90 text-accent-primary-text font-medium px-3 py-1.5 rounded-md transition-opacity"
					>
						Sign In
					</a>
				{/if}
			</div>
		</header>
	{/if}

	{#if isOffline && auth.hasCachedSession}
		<div
			data-testid="offline-banner"
			class="flex flex-wrap items-center justify-between gap-2 px-4 sm:px-6 py-2 bg-amber-50 dark:bg-amber-950/50 border-b border-amber-200 dark:border-amber-800/70 text-xs shrink-0"
		>
			<span class="font-medium text-amber-900 dark:text-amber-200">
				Offline mode — viewing cached budget. New transactions queue locally and sync when you reconnect.
			</span>
		</div>
	{:else if auth.sessionExpired && !auth.isAuthenticated}
		<div
			data-testid="session-expired-banner"
			class="flex flex-wrap items-center justify-between gap-2 px-4 sm:px-6 py-2 bg-rose-50 dark:bg-rose-950/50 border-b border-rose-200 dark:border-rose-800/70 text-xs shrink-0"
		>
			<span class="font-semibold text-rose-900 dark:text-rose-200">
				Your session expired. Sign in again to keep syncing.
			</span>
			<a
				href="/login"
				data-testid="session-expired-signin"
				class="px-2.5 py-1 rounded-md font-semibold bg-rose-600 hover:bg-rose-500 text-white transition-colors"
			>
				Sign In
			</a>
		</div>
	{/if}

	{#if auth.isAuthenticated && demo.isDemo}
		<div
			data-testid="demo-mode-banner"
			class="flex flex-wrap items-center justify-between gap-2 px-4 sm:px-6 py-2 bg-amber-50 dark:bg-amber-950/50 border-b border-amber-200 dark:border-amber-800/70 text-xs shrink-0"
		>
			<div class="flex flex-wrap items-center gap-2 min-w-0">
				<span class="font-bold tracking-wide text-amber-900 dark:text-amber-200">
					DEMO MODE · Isolated Sandbox
				</span>
				{#if demo.unsyncedCount > 0}
					<span class="px-2 py-0.5 rounded-full text-[11px] font-semibold bg-amber-200/80 text-amber-950 dark:bg-amber-500/30 dark:text-amber-100">
						{demo.unsyncedCount} staged
					</span>
				{/if}
				{#if demo.syncStatus !== 'idle'}
					<span
						class="px-2 py-0.5 rounded-full text-[11px] font-medium {demo.syncStatus === 'failed'
							? 'bg-rose-100 text-rose-800 dark:bg-rose-500/20 dark:text-rose-200'
							: demo.syncStatus === 'retrying'
								? 'bg-amber-200 text-amber-950 dark:bg-amber-500/30 dark:text-amber-100'
								: demo.syncStatus === 'completed'
									? 'bg-emerald-100 text-emerald-800 dark:bg-emerald-500/20 dark:text-emerald-200'
									: 'bg-sky-100 text-sky-800 dark:bg-sky-500/20 dark:text-sky-200'}"
					>
						{demo.syncMessage}
					</span>
				{/if}
			</div>

			<div class="flex items-center gap-1.5 shrink-0">
				<button
					type="button"
					onclick={() => demo.openDrawer()}
					data-testid="banner-open-simulator-btn"
					class="px-2.5 py-1 rounded-md font-semibold bg-amber-900 text-white hover:bg-amber-800 dark:bg-amber-500 dark:text-slate-950 dark:hover:bg-amber-400 transition-colors"
				>
					Open Simulator
				</button>
				<button
					type="button"
					disabled={isSyncing}
					onclick={() => void demo.triggerSyncAndPoll()}
					data-testid="banner-sync-now-btn"
					class="px-2.5 py-1 rounded-md font-semibold border border-amber-300 dark:border-amber-700 bg-white dark:bg-slate-900 text-amber-900 dark:text-amber-200 hover:bg-amber-100 dark:hover:bg-amber-900/40 disabled:opacity-50 transition-colors"
				>
					{isSyncing ? 'Syncing...' : 'Sync Now'}
				</button>
				<button
					type="button"
					disabled={demo.loading}
					onclick={() => void demo.toggleDemo(false)}
					data-testid="banner-exit-demo-btn"
					class="px-2.5 py-1 rounded-md font-medium text-amber-800 dark:text-amber-300 hover:bg-amber-200/60 dark:hover:bg-amber-800/40 transition-colors"
				>
					Exit Demo
				</button>
			</div>
		</div>
	{/if}

	{#if auth.isAuthenticated}
		<InstallPromptBanner />
	{/if}

	<main class="flex-1 overflow-y-auto overscroll-contain-scroll">
		{@render children()}
	</main>

	{#if auth.isAuthenticated && demo.isDemo}
		<DemoSimulatorDrawer />
	{/if}

	<PushPromptModal
		open={pushModalOpen}
		vapidPublicKey={pushConfig?.vapid_public_key ?? ''}
		serverEnabled={pushConfig?.enabled ?? false}
		environment={pushEnv}
		onClose={() => (pushModalOpen = false)}
		onDismiss={() => dismissPushPrompt()}
		onSubscribed={() => {
			dismissPushPrompt();
		}}
	/>
</div>
{/if}
