<script lang="ts">
	import { auth } from '$lib/stores/auth.svelte';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';

	let isSubmitting = $state(false);
	let errorMsg = $state('');

	onMount(() => {
		if (auth.isAuthenticated) {
			goto('/dashboard');
		}
	});

	async function handleLogin() {
		isSubmitting = true;
		errorMsg = '';
		try {
			await auth.login();
		} catch (err: any) {
			console.error('Login redirect failed:', err);
			errorMsg = err?.message || 'Failed to initiate login redirect.';
			isSubmitting = false;
		}
	}
</script>

<div class="min-h-full flex items-center justify-center p-4">
	<div class="w-full max-w-sm bg-slate-900 border border-slate-800 rounded-xl p-6 shadow-2xl">
		<div class="text-center mb-6">
			<div class="inline-flex w-12 h-12 rounded-xl bg-emerald-500/10 text-emerald-400 items-center justify-center font-bold text-2xl border border-emerald-500/20 mb-3">
				$
			</div>
			<h2 class="text-xl font-bold text-white">Welcome Back</h2>
			<p class="text-xs text-slate-400 mt-1">Authenticate via your local Dex OIDC provider</p>
		</div>

		{#if errorMsg}
			<div class="mb-4 p-3 rounded-lg bg-rose-500/10 border border-rose-500/30 text-rose-300 text-xs">
				{errorMsg}
			</div>
		{/if}

		<button
			onclick={handleLogin}
			disabled={isSubmitting}
			class="w-full py-2.5 px-4 bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 text-white font-medium rounded-lg text-sm transition shadow-lg shadow-emerald-950 flex items-center justify-center gap-2"
		>
			{#if isSubmitting}
				<div class="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin"></div>
				<span>Redirecting to Dex...</span>
			{:else}
				<span>Sign In with Dex OIDC</span>
			{/if}
		</button>

		<div class="mt-6 pt-4 border-t border-slate-800 text-xs text-slate-400 space-y-1">
			<p class="font-medium text-slate-300">Local Dev Credentials:</p>
			<p>Email: <code class="text-emerald-400 bg-slate-950 px-1 py-0.5 rounded">admin@dev.local</code></p>
			<p>Password: <code class="text-emerald-400 bg-slate-950 px-1 py-0.5 rounded">password</code></p>
		</div>
	</div>
</div>
