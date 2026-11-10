<script lang="ts">
	import { auth } from '$lib/stores/auth.svelte';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';

	let workspaceData = $state<{ workspace_id: string; inbound_receipt_mail: string; is_new_user: boolean } | null>(null);
	let isLoading = $state(false);
	let apiError = $state('');

	onMount(async () => {
		if (!auth.isAuthenticated) {
			// In dev mode allow viewing if offline/dev session
		}

		if (auth.token) {
			isLoading = true;
			try {
				const res = await fetch('http://localhost:8080/api/v1/workspaces/me', {
					headers: {
						Authorization: `Bearer ${auth.token}`
					}
				});
				if (res.ok) {
					workspaceData = await res.json();
				}
			} catch (err: any) {
				console.warn('Backend not reachable or offline:', err);
				apiError = 'Could not reach backend API server.';
			} finally {
				isLoading = false;
			}
		}
	});
</script>

<div class="p-6 max-w-4xl mx-auto space-y-6">
	<div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4 bg-slate-900 border border-slate-800 p-5 rounded-2xl">
		<div>
			<h2 class="text-xl font-bold text-white tracking-tight">Financial Overview</h2>
			<p class="text-xs text-slate-400 mt-1">Zero-based allocation ledger &amp; solvency status</p>
		</div>
		<div class="flex items-center gap-2">
			<span class="inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-medium bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
				<span class="w-1.5 h-1.5 rounded-full bg-emerald-400"></span>
				Ready to Assign: $0.00
			</span>
		</div>
	</div>

	<div class="grid grid-cols-1 md:grid-cols-3 gap-4">
		<div class="bg-slate-900/60 border border-slate-800/80 p-4 rounded-xl">
			<p class="text-xs text-slate-400 font-medium">Session Status</p>
			<p class="text-base font-semibold text-white mt-1">
				{#if auth.isAuthenticated}
					<span class="text-emerald-400">Authenticated</span>
				{:else}
					<span class="text-amber-400">Guest / Offline</span>
				{/if}
			</p>
			<p class="text-xs text-slate-500 mt-1 truncate">User: {auth.user?.email || 'admin@dev.local'}</p>
		</div>

		<div class="bg-slate-900/60 border border-slate-800/80 p-4 rounded-xl">
			<p class="text-xs text-slate-400 font-medium">Workspace Provisioning</p>
			<p class="text-base font-semibold text-white mt-1">
				{#if workspaceData}
					<span class="text-emerald-400">Active</span>
				{:else if isLoading}
					<span class="text-slate-400">Checking...</span>
				{:else}
					<span class="text-slate-400">Personal Workspace</span>
				{/if}
			</p>
			<p class="text-xs text-slate-500 mt-1 truncate">
				{workspaceData?.inbound_receipt_mail || 'w_default@receipts.yourdomain.com'}
			</p>
		</div>

		<div class="bg-slate-900/60 border border-slate-800/80 p-4 rounded-xl">
			<p class="text-xs text-slate-400 font-medium">Solvency Buffer</p>
			<p class="text-base font-semibold text-emerald-400 mt-1">$0.00</p>
			<p class="text-xs text-slate-500 mt-1">Liquid Cash &ge; Card Balances</p>
		</div>
	</div>

	<div class="bg-slate-900 border border-slate-800 rounded-2xl p-6 text-center space-y-3">
		<h3 class="text-sm font-semibold text-slate-200">Milestone 1 Foundation Complete</h3>
		<p class="text-xs text-slate-400 max-w-md mx-auto">
			PostgreSQL RLS tenancy, Aspire orchestration, Go Chi backend, OIDC PKCE authentication, and SvelteKit PWA scaffold are operational.
		</p>
	</div>
</div>
