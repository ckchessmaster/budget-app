<script lang="ts">
	import '../app.css';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { auth } from '$lib/stores/auth.svelte';
	import { theme } from '$lib/stores/theme.svelte';

	let { children } = $props();

	const PUBLIC_PATHS = ['/login', '/auth/callback'];

	let currentPath = $derived(page.url.pathname);
	let isBudgetRoute = $derived(currentPath === '/budget' || currentPath.startsWith('/budget/'));
	let isBudgetActive = $derived(currentPath === '/' || currentPath.startsWith('/budget'));
	let isAccountsActive = $derived(currentPath.startsWith('/accounts'));

	onMount(() => {
		theme.init();
	});

	$effect(() => {
		const path = page.url.pathname;
		if (PUBLIC_PATHS.some((p) => path.startsWith(p))) {
			return;
		}
		void auth.ready().then(() => {
			if (!auth.isAuthenticated) {
				void goto('/login');
			}
		});
	});
</script>

<div class="flex flex-col h-dvh w-full bg-canvas text-ink-primary overflow-hidden font-sans">
	{#if !isBudgetRoute}
		<header class="flex items-center justify-between gap-4 px-4 sm:px-6 bg-surface border-b border-border shrink-0 select-none">
			<div class="flex items-center gap-6">
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
				</nav>
			</div>

			<div class="flex items-center gap-2 sm:gap-3">
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
						onclick={() => auth.logout()}
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

	<main class="flex-1 overflow-y-auto overscroll-contain-scroll">
		{@render children()}
	</main>
</div>
