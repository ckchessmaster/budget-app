<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';

	onMount(() => {
		void goto('/budget?panel=transactions', { replaceState: true });
	});
</script>

<div class="h-full flex items-center justify-center">
	<div class="w-6 h-6 border-2 border-border border-t-accent-pop rounded-full animate-spin"></div>
</div>
