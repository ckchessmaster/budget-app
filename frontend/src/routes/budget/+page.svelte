<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { fetchMonthBudget } from '$lib/api/budget';
	import BudgetHeader from '$lib/components/budget/BudgetHeader.svelte';
	import CategoryModal from '$lib/components/budget/CategoryModal.svelte';
	import CategoryRow from '$lib/components/budget/CategoryRow.svelte';
	import CoverDrawer from '$lib/components/budget/CoverDrawer.svelte';
	import IncomeModal from '$lib/components/budget/IncomeModal.svelte';
	import type { CategoryBudget, MonthBudgetDetails } from '$lib/types/budget';

	function defaultMonth(): string {
		const now = new Date();
		return `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}`;
	}

	let currentMonth = $derived.by(() => {
		const q = page.url.searchParams.get('month');
		if (q && /^\d{4}-\d{2}$/.test(q)) return q;
		return defaultMonth();
	});

	let budgetData = $state<MonthBudgetDetails | null>(null);
	let isLoading = $state(false);
	let errorMsg = $state('');

	let coverCategoryId = $state<string | null>(null);
	let isCategoryModalOpen = $state(false);
	let editingCategory = $state<CategoryBudget | null>(null);
	let isIncomeModalOpen = $state(false);

	async function loadBudget(month: string) {
		isLoading = true;
		errorMsg = '';
		try {
			budgetData = await fetchMonthBudget(month);
		} catch (err: any) {
			errorMsg = err?.message || 'Could not load budget data.';
		} finally {
			isLoading = false;
		}
	}

	$effect(() => {
		loadBudget(currentMonth);
	});

	function handleMonthChange(nextMonth: string) {
		goto(`/budget?month=${nextMonth}`, { replaceState: false, keepFocus: true, noScroll: true });
	}

	let groupedCategories = $derived.by(() => {
		const groups = new Map<string, CategoryBudget[]>();
		for (const cat of budgetData?.categories ?? []) {
			const list = groups.get(cat.groupName) ?? [];
			list.push(cat);
			groups.set(cat.groupName, list);
		}
		return Array.from(groups.entries());
	});

	let overspentCategory = $derived.by(() => {
		if (!coverCategoryId || !budgetData) return null;
		const found = budgetData.categories.find((c) => c.id === coverCategoryId);
		if (!found) return null;
		return { id: found.id, name: found.name, remaining: found.remaining };
	});

	let availableDonors = $derived.by(() => {
		if (!budgetData) return [];
		return budgetData.categories
			.filter((c) => c.id !== coverCategoryId && c.remaining > 0)
			.map((c) => ({ id: c.id, name: c.name, available: c.remaining }));
	});

	function openEditCategory(catId: string) {
		const found = budgetData?.categories.find((c) => c.id === catId) ?? null;
		editingCategory = found;
		isCategoryModalOpen = true;
	}
</script>

<div class="p-4 sm:p-6 max-w-4xl mx-auto space-y-6">
	<BudgetHeader
		{currentMonth}
		summary={budgetData?.summary ?? null}
		onMonthChange={handleMonthChange}
		onOpenIncomes={() => (isIncomeModalOpen = true)}
		onOpenNewCategory={() => {
			editingCategory = null;
			isCategoryModalOpen = true;
		}}
	/>

	{#if errorMsg}
		<div class="rounded-xl border border-rose-500/30 bg-rose-500/10 p-4 text-xs text-rose-300">
			{errorMsg}
		</div>
	{/if}

	{#if isLoading && !budgetData}
		<div class="rounded-2xl border border-slate-800 bg-slate-900 p-8 text-center text-sm text-slate-400">
			Loading {currentMonth} budget...
		</div>
	{:else if groupedCategories.length === 0}
		<div class="rounded-2xl border border-slate-800 bg-slate-900 p-8 text-center space-y-3">
			<p class="text-sm font-medium text-slate-200">No categories yet</p>
			<p class="text-xs text-slate-400">
				Add your first budget category or planned income to start assigning every dollar.
			</p>
		</div>
	{:else}
		<div class="space-y-6">
			{#each groupedCategories as [groupName, cats]}
				<section class="space-y-2.5">
					<h3 class="text-xs font-semibold uppercase tracking-wider text-slate-400 px-1">
						{groupName}
					</h3>
					<div class="space-y-2.5">
						{#each cats as cat (cat.id)}
							<CategoryRow
								category={cat}
								onCoverClick={(id) => (coverCategoryId = id)}
								onEditClick={openEditCategory}
							/>
						{/each}
					</div>
				</section>
			{/each}
		</div>
	{/if}
</div>

<CoverDrawer
	isOpen={coverCategoryId !== null}
	yearMonth={`${currentMonth}-01`}
	{overspentCategory}
	{availableDonors}
	onClose={() => (coverCategoryId = null)}
	onSuccess={() => {
		coverCategoryId = null;
		loadBudget(currentMonth);
	}}
/>

<CategoryModal
	isOpen={isCategoryModalOpen}
	{editingCategory}
	allCategories={budgetData?.categories ?? []}
	onClose={() => (isCategoryModalOpen = false)}
	onSuccess={() => {
		isCategoryModalOpen = false;
		loadBudget(currentMonth);
	}}
/>

<IncomeModal
	isOpen={isIncomeModalOpen}
	yearMonth={`${currentMonth}-01`}
	incomes={budgetData?.incomes ?? []}
	onClose={() => (isIncomeModalOpen = false)}
	onChanged={() => loadBudget(currentMonth)}
/>
