<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/stores/auth.svelte';

	let errorMsg = $state('');

	onMount(async () => {
		try {
			await auth.handleCallback();
			await goto('/dashboard');
		} catch (err: any) {
			console.error('OIDC callback failed:', err);
			errorMsg = err?.message || 'Authentication callback failed.';
		}
	});
</script>

<div class="min-h-full flex items-center justify-center p-4">
	<div class="text-center space-y-4 max-w-sm">
		{#if errorMsg}
			<div class="p-4 rounded-xl bg-rose-500/10 border border-rose-500/30 text-rose-300 text-sm">
				<p class="font-semibold mb-1">Authentication Error</p>
				<p class="text-xs">{errorMsg}</p>
				<a href="/login" class="inline-block mt-3 text-xs text-white bg-slate-800 px-3 py-1.5 rounded hover:bg-slate-700">
					Return to Login
				</a>
			</div>
		{:else}
			<div class="inline-block w-8 h-8 border-2 border-emerald-500/30 border-t-emerald-500 rounded-full animate-spin"></div>
			<p class="text-sm text-slate-300">Completing authentication handshake...</p>
		{/if}
	</div>
</div>
