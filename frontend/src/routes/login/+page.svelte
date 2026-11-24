<script lang="ts">
	import { auth } from '$lib/stores/auth.svelte';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';

	let isSubmitting = $state(false);
	let errorMsg = $state('');

	onMount(() => {
		if (auth.isAuthenticated) {
			goto('/budget');
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
	<div class="w-full max-w-sm bg-surface border border-border rounded-xl p-6 shadow-xl">
		<div class="text-center mb-6">
			<div class="inline-flex w-12 h-12 rounded-xl bg-accent-primary text-accent-primary-text items-center justify-center font-bold text-2xl mb-3">
				$
			</div>
			<h2 class="text-xl font-bold text-ink-primary">Welcome Back</h2>
			<p class="text-xs text-ink-secondary mt-1">Authenticate via your local Dex OIDC provider</p>
		</div>

		{#if errorMsg}
			<div class="mb-4 p-3 rounded-lg bg-status-danger-bg border border-status-danger/30 text-status-danger text-xs">
				{errorMsg}
			</div>
		{/if}

		<button
			onclick={handleLogin}
			disabled={isSubmitting}
			class="w-full py-2.5 px-4 bg-accent-primary hover:opacity-90 disabled:opacity-50 text-accent-primary-text font-medium rounded-lg text-sm transition-opacity shadow-sm flex items-center justify-center gap-2"
		>
			{#if isSubmitting}
				<div class="w-4 h-4 border-2 border-accent-primary-text/30 border-t-accent-primary-text rounded-full animate-spin"></div>
				<span>Redirecting to Dex...</span>
			{:else}
				<span>Sign In with Dex OIDC</span>
			{/if}
		</button>

		<div class="mt-6 pt-4 border-t border-border text-xs text-ink-secondary space-y-1">
			<p class="font-medium text-ink-primary">Local Dev Credentials:</p>
			<p>Email: <code class="text-accent-pop bg-subtle px-1 py-0.5 rounded font-mono">admin@dev.local</code></p>
			<p>Password: <code class="text-accent-pop bg-subtle px-1 py-0.5 rounded font-mono">password</code></p>
		</div>
	</div>
</div>
