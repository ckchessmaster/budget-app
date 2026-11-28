<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/stores/auth.svelte';

	let { children } = $props();
	let checking = $state(true);
	let authorized = $state(false);

	onMount(() => {
		void (async () => {
			await auth.ready();
			if (!auth.isAuthenticated) {
				await goto('/login');
				return;
			}
			await auth.ensureServerIdentity();
			if (!auth.isPlatformAdmin) {
				await goto('/');
				return;
			}
			authorized = true;
			checking = false;
		})();
	});
</script>

{#if checking}
	<div class="min-h-full flex items-center justify-center p-6 text-xs text-ink-secondary">
		Verifying administrator access...
	</div>
{:else if authorized}
	{@render children()}
{/if}
