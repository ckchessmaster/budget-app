<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/stores/auth.svelte';

	let errorMsg = $state('');

	onMount(async () => {
		try {
			await auth.handleCallback();
			await goto('/budget');
		} catch (err: any) {
			console.error('OIDC callback failed:', err);
			errorMsg = err?.message || 'Authentication callback failed.';
		}
	});
</script>

<div class="min-h-full flex items-center justify-center p-4">
	<div class="text-center space-y-4 max-w-sm">
		{#if errorMsg}
			<div class="p-4 rounded-xl bg-status-danger-bg border border-status-danger/30 text-status-danger text-sm">
				<p class="font-semibold mb-1">Authentication Error</p>
				<p class="text-xs">{errorMsg}</p>
				<a
					href="/login"
					class="inline-block mt-3 text-xs text-ink-primary bg-surface border border-border px-3 py-1.5 rounded hover:bg-surface-hover transition-colors"
				>
					Return to Login
				</a>
			</div>
		{:else}
			<div class="inline-block w-8 h-8 border-2 border-border border-t-accent-pop rounded-full animate-spin"></div>
			<p class="text-sm text-ink-secondary">Completing authentication handshake...</p>
		{/if}
	</div>
</div>
