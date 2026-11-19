<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';

	onMount(() => {
		void goto('/budget', { replaceState: true });
	});
</script>

<div class="h-full flex items-center justify-center">
	<div class="w-6 h-6 border-2 border-emerald-500/30 border-t-emerald-600 dark:border-t-emerald-500 rounded-full animate-spin"></div>
</div>
