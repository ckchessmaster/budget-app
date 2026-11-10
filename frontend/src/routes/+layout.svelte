<script lang="ts">
	import '../app.css';
	import { auth } from '$lib/stores/auth.svelte';

	let { children } = $props();
</script>

<div class="flex flex-col h-dvh w-full bg-slate-950 text-slate-100 overflow-hidden font-sans">
	<header class="flex items-center justify-between px-4 py-3 bg-slate-900 border-b border-slate-800 shrink-0 select-none">
		<div class="flex items-center gap-2">
			<div class="w-8 h-8 rounded-lg bg-emerald-500/20 text-emerald-400 flex items-center justify-center font-bold text-lg border border-emerald-500/30">
				$
			</div>
			<div>
				<h1 class="text-base font-semibold leading-none tracking-tight">Budget App</h1>
				<p class="text-xs text-slate-400">Zero-Based Financial Ledger</p>
			</div>
		</div>

		<nav class="flex items-center gap-3">
			{#if auth.isAuthenticated}
				<span class="text-xs bg-slate-800 text-slate-300 px-2 py-1 rounded-full border border-slate-700 font-mono">
					{auth.user?.email || auth.user?.id.slice(0, 8)}
				</span>
				<button
					onclick={() => auth.logout()}
					class="text-xs text-slate-400 hover:text-slate-100 px-2 py-1 rounded hover:bg-slate-800 transition-colors"
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
		</nav>
	</header>

	<main class="flex-1 overflow-y-auto">
		{@render children()}
	</main>
</div>
