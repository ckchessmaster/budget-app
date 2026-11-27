<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { fetchMonthBudget } from '$lib/api/budget';
	import { fetchAccounts } from '$lib/api/transactions';
	import CategorizationRulesSection from '$lib/components/settings/CategorizationRulesSection.svelte';
	import { auth } from '$lib/stores/auth.svelte';
	import type { CategoryBudget, PlannedIncome } from '$lib/types/budget';
	import type { AccountItem } from '$lib/types/transaction';

	let accounts = $state<AccountItem[]>([]);
	let categories = $state<CategoryBudget[]>([]);
	let incomes = $state<PlannedIncome[]>([]);
	let loading = $state(true);

	onMount(async () => {
		await auth.ready();
		if (!auth.isAuthenticated && !auth.canAccessOffline) {
			void goto('/login');
			return;
		}
		const now = new Date();
		const ym = `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}`;
		try {
			const [accts, budgetRes] = await Promise.all([
				fetchAccounts().catch(() => []),
				fetchMonthBudget(ym).catch(() => null)
			]);
			accounts = accts;
			if (budgetRes) {
				categories = budgetRes.categories;
				incomes = budgetRes.incomes;
			}
		} finally {
			loading = false;
		}
	});
</script>

<div class="max-w-4xl mx-auto p-4 sm:p-6 space-y-5">
	<div class="rounded-2xl border border-border bg-surface p-5 shadow-xs">
		{#if loading}
			<p class="text-xs text-ink-secondary">Loading rules...</p>
		{:else}
			<CategorizationRulesSection {categories} {incomes} {accounts} />
		{/if}
	</div>
</div>
