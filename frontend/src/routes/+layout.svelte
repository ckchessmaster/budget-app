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

<div class="flex flex-col h-dvh w-full bg-slate-100 text-slate-900 dark:bg-slate-950 dark:text-slate-100 overflow-hidden font-sans">
	<header class="flex items-center justify-between gap-4 px-4 sm:px-6 bg-white/95 dark:bg-slate-900/95 backdrop-blur-sm border-b border-slate-200 dark:border-slate-800 shrink-0 select-none">
		<div class="flex items-center gap-6">
			<a href="/budget" class="flex items-center gap-2.5 py-2.5 group">
				<div class="w-8 h-8 rounded-lg bg-emerald-500/15 text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-400 flex items-center justify-center font-bold text-lg border border-emerald-500/30 group-hover:border-emerald-500/60 transition-colors">
					$
				</div>
				<div class="hidden sm:block">
					<h1 class="text-sm font-bold leading-none tracking-tight text-slate-900 dark:text-white">
						Budget App
					</h1>
					<p class="text-[11px] text-slate-500 dark:text-slate-400 mt-0.5">Zero-Based Ledger</p>
				</div>
			</a>

			<nav aria-label="Primary" class="flex items-center gap-1">
				<a
					href="/budget"
					aria-current={isBudgetActive ? 'page' : undefined}
					class="relative px-3 py-3.5 text-xs font-semibold transition-colors {isBudgetActive
						? 'text-emerald-700 dark:text-emerald-400 after:content-[""] after:absolute after:inset-x-2 after:bottom-0 after:h-0.5 after:rounded-full after:bg-emerald-600 dark:after:bg-emerald-400'
						: 'text-slate-600 hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100'}"
				>
					Budget
				</a>
				<a
					href="/accounts"
					aria-current={isAccountsActive ? 'page' : undefined}
					class="relative px-3 py-3.5 text-xs font-semibold transition-colors {isAccountsActive
						? 'text-emerald-700 dark:text-emerald-400 after:content-[""] after:absolute after:inset-x-2 after:bottom-0 after:h-0.5 after:rounded-full after:bg-emerald-600 dark:after:bg-emerald-400'
						: 'text-slate-600 hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100'}"
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
				class="p-2 rounded-lg border border-slate-200 dark:border-slate-700 bg-slate-50 hover:bg-slate-100 dark:bg-slate-800/80 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-200 transition-colors"
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
				<span class="hidden md:inline-block text-xs bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-300 px-2.5 py-1 rounded-full border border-slate-200 dark:border-slate-700 font-mono">
					{auth.user?.email || auth.user?.id.slice(0, 8)}
				</span>
				<button
					type="button"
					onclick={() => auth.logout()}
					class="text-xs text-slate-600 hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100 px-2.5 py-1.5 rounded-md hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
				>
					Sign Out
				</button>
			{:else}
				<a
					href="/login"
					class="text-xs bg-emerald-600 hover:bg-emerald-500 text-white font-medium px-3 py-1.5 rounded-md transition-colors"
				>
					Sign In
				</a>
			{/if}
		</div>
	</header>

	<main class="flex-1 overflow-y-auto overscroll-contain-scroll">
		{@render children()}
	</main>
</div>
