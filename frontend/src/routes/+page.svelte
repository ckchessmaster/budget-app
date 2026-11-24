<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/stores/auth.svelte';

	onMount(async () => {
		await auth.ready();
		if (auth.isAuthenticated) {
			goto('/budget');
		} else {
			goto('/login');
		}
	});
</script>

<div class="h-full flex items-center justify-center">
	<div class="w-6 h-6 border-2 border-border border-t-accent-pop rounded-full animate-spin"></div>
</div>
