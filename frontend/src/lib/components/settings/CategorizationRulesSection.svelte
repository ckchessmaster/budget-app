<script lang="ts">
	import { onMount } from 'svelte';
	import {
		applyCategorizationRule,
		deleteCategorizationRule,
		fetchCategorizationRules,
		reorderCategorizationRules,
		updateCategorizationRule
	} from '$lib/api/categorization';
	import type { CategoryBudget, PlannedIncome } from '$lib/types/budget';
	import type { CategorizationRule } from '$lib/types/categorization';
	import type { AccountItem } from '$lib/types/transaction';
	import RuleModal from './RuleModal.svelte';

	interface Props {
		categories: CategoryBudget[];
		incomes?: PlannedIncome[];
		accounts: AccountItem[];
		initialRules?: CategorizationRule[];
	}

	let { categories, incomes = [], accounts, initialRules }: Props = $props();

	let rules = $state<CategorizationRule[]>([]);
	let loading = $state(false);
	let errorMsg = $state<string | null>(null);
	let feedbackMsg = $state<string | null>(null);
	let busyId = $state<string | null>(null);
	let isModalOpen = $state(false);
	let editingRule = $state<CategorizationRule | null>(null);

	let categoryById = $derived(new Map(categories.map((c) => [c.id, c])));
	let accountById = $derived(new Map(accounts.map((a) => [a.id, a])));

	$effect(() => {
		if (initialRules) {
			rules = initialRules;
		}
	});

	export async function refreshRules() {
		loading = true;
		errorMsg = null;
		try {
			rules = await fetchCategorizationRules();
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to load categorization rules.';
		} finally {
			loading = false;
		}
	}

	onMount(() => {
		if (!initialRules) {
			void refreshRules();
		}
	});

	async function handleToggleActive(rule: CategorizationRule) {
		busyId = rule.id;
		errorMsg = null;
		feedbackMsg = null;
		try {
			const updated = await updateCategorizationRule(rule.id, {
				matchType: rule.matchType,
				merchantPattern: rule.merchantPattern,
				accountId: rule.accountId,
				direction: rule.direction,
				minAmount: rule.minAmount,
				maxAmount: rule.maxAmount,
				categoryId: rule.categoryId,
				plannedIncomeName: rule.plannedIncomeName,
				renameMerchant: rule.renameMerchant,
				requireReview: rule.requireReview,
				isActive: !rule.isActive
			});
			rules = rules.map((r) => (r.id === rule.id ? updated : r));
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to update rule.';
		} finally {
			busyId = null;
		}
	}

	async function handleMove(index: number, delta: -1 | 1) {
		const target = index + delta;
		if (target < 0 || target >= rules.length) return;
		const reordered = [...rules];
		const [item] = reordered.splice(index, 1);
		reordered.splice(target, 0, item);
		rules = reordered;

		busyId = item.id;
		errorMsg = null;
		try {
			rules = await reorderCategorizationRules(reordered.map((r) => r.id));
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to reorder rules.';
			await refreshRules();
		} finally {
			busyId = null;
		}
	}

	async function handleApplyNow(rule: CategorizationRule) {
		busyId = rule.id;
		errorMsg = null;
		feedbackMsg = null;
		try {
			const count = await applyCategorizationRule(rule.id);
			feedbackMsg =
				count > 0
					? `Rule "${rule.merchantPattern}" applied to ${count} uncategorized transaction${count === 1 ? '' : 's'}.`
					: `No uncategorized transactions matched "${rule.merchantPattern}".`;
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to apply rule.';
		} finally {
			busyId = null;
		}
	}

	async function handleDelete(rule: CategorizationRule) {
		busyId = rule.id;
		errorMsg = null;
		feedbackMsg = null;
		try {
			await deleteCategorizationRule(rule.id);
			rules = rules.filter((r) => r.id !== rule.id);
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to delete rule.';
		} finally {
			busyId = null;
		}
	}

	function formatTarget(rule: CategorizationRule): string {
		if (rule.plannedIncomeName) return `Income: ${rule.plannedIncomeName}`;
		if (rule.categoryId) return categoryById.get(rule.categoryId)?.name ?? 'Category';
		return 'Unassigned';
	}

	function formatConditions(rule: CategorizationRule): string[] {
		const tags: string[] = [];
		tags.push(rule.matchType === 'EXACT' ? 'Exact' : 'Contains');
		if (rule.direction !== 'ANY') {
			tags.push(rule.direction === 'EXPENSE' ? 'Expense' : 'Inflow');
		}
		if (rule.accountId) {
			tags.push(accountById.get(rule.accountId)?.name ?? 'Specific account');
		}
		if (rule.minAmount != null || rule.maxAmount != null) {
			const minStr = rule.minAmount != null ? `$${rule.minAmount}` : '$0';
			const maxStr = rule.maxAmount != null ? `$${rule.maxAmount}` : '∞';
			tags.push(`${minStr}–${maxStr}`);
		}
		if (rule.requireReview) {
			tags.push('Requires review');
		}
		return tags;
	}
</script>

<div class="space-y-4" data-testid="categorization-rules-section">
	<div class="flex flex-wrap items-start justify-between gap-3">
		<div>
			<h3 class="text-base font-bold text-slate-900 dark:text-white">Categorization Rules</h3>
			<p class="text-xs text-slate-500 dark:text-slate-400">
				Priority-ordered rules run automatically on uncategorized transactions during bank sync.
			</p>
		</div>

		<button
			type="button"
			data-testid="new-rule-btn"
			onclick={() => {
				editingRule = null;
				isModalOpen = true;
			}}
			class="px-3 py-1.5 rounded-lg text-xs font-semibold bg-emerald-600 hover:bg-emerald-500 text-white"
		>
			+ New Rule
		</button>
	</div>

	{#if errorMsg}
		<div role="alert" class="rounded-lg border border-rose-200 dark:border-rose-500/30 bg-rose-50 dark:bg-rose-500/10 px-3 py-2 text-xs text-rose-700 dark:text-rose-300">
			{errorMsg}
		</div>
	{/if}

	{#if feedbackMsg}
		<div class="rounded-lg border border-emerald-200 dark:border-emerald-500/30 bg-emerald-50 dark:bg-emerald-500/10 px-3 py-2 text-xs text-emerald-800 dark:text-emerald-200">
			{feedbackMsg}
		</div>
	{/if}

	{#if loading && rules.length === 0}
		<p class="text-xs text-slate-500 dark:text-slate-400">Loading rules...</p>
	{:else if rules.length === 0}
		<div class="rounded-xl border border-dashed border-slate-300 dark:border-slate-700 p-5 text-center space-y-1">
			<p class="text-xs font-medium text-slate-700 dark:text-slate-200">No categorization rules yet</p>
			<p class="text-[11px] text-slate-500 dark:text-slate-400">
				Create a rule here or click "Always apply rule" right after categorizing a transaction.
			</p>
		</div>
	{:else}
		<div class="divide-y divide-slate-200 dark:divide-slate-800 rounded-xl border border-slate-200 dark:border-slate-800 overflow-hidden">
			{#each rules as rule, idx (rule.id)}
				<div
					data-testid="rule-row"
					class="p-3.5 flex flex-wrap items-center justify-between gap-3 bg-white dark:bg-slate-900 {!rule.isActive
						? 'opacity-60'
						: ''}"
				>
					<div class="min-w-0 flex-1 space-y-1">
						<div class="flex items-center gap-2 flex-wrap">
							<span class="text-[10px] font-mono text-slate-400">#{idx + 1}</span>
							<span class="text-xs font-semibold text-slate-900 dark:text-white">
								"{rule.merchantPattern}"
							</span>
							<span class="text-xs text-slate-400">&rarr;</span>
							<span class="inline-flex items-center rounded-full bg-emerald-50 dark:bg-emerald-500/15 text-emerald-700 dark:text-emerald-300 px-2 py-0.5 text-[11px] font-semibold">
								{formatTarget(rule)}
							</span>
							{#if rule.renameMerchant}
								<span class="text-[11px] text-slate-500 dark:text-slate-400">
									(Rename: {rule.renameMerchant})
								</span>
							{/if}
						</div>

						<div class="flex flex-wrap items-center gap-1.5">
							{#each formatConditions(rule) as tag (tag)}
								<span class="text-[10px] px-1.5 py-0.5 rounded bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-300">
									{tag}
								</span>
							{/each}
						</div>
					</div>

					<div class="flex items-center gap-1.5 shrink-0">
						<button
							type="button"
							aria-label={`Move rule ${idx + 1} up`}
							disabled={idx === 0 || busyId === rule.id}
							onclick={() => void handleMove(idx, -1)}
							class="px-2 py-1 rounded border border-slate-200 dark:border-slate-700 text-[11px] text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-800 disabled:opacity-40"
						>
							↑
						</button>
						<button
							type="button"
							aria-label={`Move rule ${idx + 1} down`}
							disabled={idx === rules.length - 1 || busyId === rule.id}
							onclick={() => void handleMove(idx, 1)}
							class="px-2 py-1 rounded border border-slate-200 dark:border-slate-700 text-[11px] text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-800 disabled:opacity-40"
						>
							↓
						</button>
						<button
							type="button"
							disabled={busyId === rule.id}
							onclick={() => void handleToggleActive(rule)}
							class="px-2 py-1 rounded border border-slate-200 dark:border-slate-700 text-[11px] font-medium text-slate-700 dark:text-slate-200 hover:bg-slate-50 dark:hover:bg-slate-800"
						>
							{rule.isActive ? 'Enabled' : 'Paused'}
						</button>
						<button
							type="button"
							disabled={busyId === rule.id}
							onclick={() => void handleApplyNow(rule)}
							class="px-2 py-1 rounded border border-slate-200 dark:border-slate-700 text-[11px] font-medium text-emerald-700 dark:text-emerald-300 hover:bg-emerald-50 dark:hover:bg-emerald-500/10"
						>
							Apply Now
						</button>
						<button
							type="button"
							disabled={busyId === rule.id}
							onclick={() => {
								editingRule = rule;
								isModalOpen = true;
							}}
							class="px-2 py-1 rounded border border-slate-200 dark:border-slate-700 text-[11px] text-slate-700 dark:text-slate-200 hover:bg-slate-50 dark:hover:bg-slate-800"
						>
							Edit
						</button>
						<button
							type="button"
							disabled={busyId === rule.id}
							onclick={() => void handleDelete(rule)}
							class="px-2 py-1 rounded border border-rose-200 dark:border-rose-500/30 text-[11px] text-rose-600 dark:text-rose-400 hover:bg-rose-50 dark:hover:bg-rose-500/10"
						>
							Delete
						</button>
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>

<RuleModal
	isOpen={isModalOpen}
	{editingRule}
	{categories}
	{incomes}
	{accounts}
	onClose={() => (isModalOpen = false)}
	onSaved={async ({ appliedCount }) => {
		await refreshRules();
		if (appliedCount > 0) {
			feedbackMsg = `Rule created and applied to ${appliedCount} existing transaction${appliedCount === 1 ? '' : 's'}.`;
		}
	}}
/>
