<script lang="ts">
	import { createCategorizationRule, updateCategorizationRule } from '$lib/api/categorization';
	import type { CategoryBudget, PlannedIncome } from '$lib/types/budget';
	import type {
		CategorizationRule,
		CreateRulePayload,
		RuleDirection,
		RuleMatchType
	} from '$lib/types/categorization';
	import type { AccountItem } from '$lib/types/transaction';

	interface Props {
		isOpen: boolean;
		editingRule?: CategorizationRule | null;
		prefill?: Partial<CreateRulePayload> | null;
		categories: CategoryBudget[];
		incomes?: PlannedIncome[];
		accounts: AccountItem[];
		onClose: () => void;
		onSaved?: (result: { rule: CategorizationRule; appliedCount: number }) => Promise<void> | void;
	}

	let {
		isOpen,
		editingRule = null,
		prefill = null,
		categories,
		incomes = [],
		accounts,
		onClose,
		onSaved
	}: Props = $props();

	let matchType = $state<RuleMatchType>('CONTAINS');
	let merchantPattern = $state('');
	let accountId = $state('');
	let direction = $state<RuleDirection>('ANY');
	let minAmount = $state('');
	let maxAmount = $state('');
	let targetKind = $state<'category' | 'income'>('category');
	let categoryId = $state('');
	let categoryFilter = $state('');
	let plannedIncomeName = $state('');
	let renameMerchant = $state('');
	let requireReview = $state(true);
	let isActive = $state(true);
	let applyNow = $state(true);
	let saving = $state(false);
	let errorMsg = $state<string | null>(null);

	let budgetAccounts = $derived(accounts.filter((a) => a.isOnBudget));
	let groupedCategories = $derived.by(() => {
		const groups = new Map<string, CategoryBudget[]>();
		for (const cat of categories) {
			const list = groups.get(cat.groupName) ?? [];
			list.push(cat);
			groups.set(cat.groupName, list);
		}
		return Array.from(groups.entries());
	});
	let filteredGroupedCategories = $derived.by(() => {
		const q = categoryFilter.trim().toLowerCase();
		if (!q) return groupedCategories;
		return groupedCategories
			.map(
				([groupName, cats]) =>
					[
						groupName,
						cats.filter(
							(c) => c.name.toLowerCase().includes(q) || groupName.toLowerCase().includes(q)
						)
					] as [string, CategoryBudget[]]
			)
			.filter(([, cats]) => cats.length > 0);
	});
	let selectedCategoryLabel = $derived(
		categories.find((c) => c.id === categoryId)?.name ?? 'Select category...'
	);

	let wasOpen = false;
	$effect(() => {
		if (isOpen && !wasOpen) {
			errorMsg = null;
			categoryFilter = '';
			if (editingRule) {
				matchType = editingRule.matchType;
				merchantPattern = editingRule.merchantPattern;
				accountId = editingRule.accountId ?? '';
				direction = editingRule.direction;
				minAmount = editingRule.minAmount != null ? String(editingRule.minAmount) : '';
				maxAmount = editingRule.maxAmount != null ? String(editingRule.maxAmount) : '';
				targetKind = editingRule.plannedIncomeName ? 'income' : 'category';
				categoryId = editingRule.categoryId ?? '';
				plannedIncomeName = editingRule.plannedIncomeName ?? '';
				renameMerchant = editingRule.renameMerchant ?? '';
				requireReview = editingRule.requireReview;
				isActive = editingRule.isActive;
				applyNow = false;
			} else {
				matchType = prefill?.matchType ?? 'CONTAINS';
				merchantPattern = prefill?.merchantPattern ?? '';
				accountId = prefill?.accountId ?? '';
				direction = prefill?.direction ?? 'ANY';
				minAmount = prefill?.minAmount != null ? String(prefill.minAmount) : '';
				maxAmount = prefill?.maxAmount != null ? String(prefill.maxAmount) : '';
				if (prefill?.plannedIncomeName) {
					targetKind = 'income';
					plannedIncomeName = prefill.plannedIncomeName;
					categoryId = '';
				} else {
					targetKind = 'category';
					categoryId = prefill?.categoryId ?? categories[0]?.id ?? '';
					plannedIncomeName = incomes[0]?.name ?? '';
				}
				renameMerchant = prefill?.renameMerchant ?? '';
				requireReview = prefill?.requireReview ?? true;
				isActive = true;
				applyNow = prefill?.applyNow ?? true;
			}
		}
		wasOpen = isOpen;
	});

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		errorMsg = null;

		const pattern = merchantPattern.trim();
		if (!pattern) {
			errorMsg = 'Merchant pattern is required.';
			return;
		}
		if (targetKind === 'category' && !categoryId) {
			errorMsg = 'Please choose a target category.';
			return;
		}
		if (targetKind === 'income' && !plannedIncomeName.trim()) {
			errorMsg = 'Please enter a planned income name.';
			return;
		}

		const minStr = String(minAmount ?? '').trim();
		const maxStr = String(maxAmount ?? '').trim();
		const parsedMin = minStr !== '' ? Number(minStr) : null;
		const parsedMax = maxStr !== '' ? Number(maxStr) : null;
		if (parsedMin !== null && (!Number.isFinite(parsedMin) || parsedMin < 0)) {
			errorMsg = 'Minimum amount must be 0 or greater.';
			return;
		}
		if (parsedMax !== null && (!Number.isFinite(parsedMax) || parsedMax < 0)) {
			errorMsg = 'Maximum amount must be 0 or greater.';
			return;
		}
		if (parsedMin !== null && parsedMax !== null && parsedMin > parsedMax) {
			errorMsg = 'Minimum amount cannot exceed maximum amount.';
			return;
		}

		saving = true;
		try {
			const basePayload = {
				matchType,
				merchantPattern: pattern,
				accountId: accountId || null,
				direction,
				minAmount: parsedMin,
				maxAmount: parsedMax,
				categoryId: targetKind === 'category' ? categoryId : null,
				plannedIncomeName: targetKind === 'income' ? plannedIncomeName.trim() : null,
				renameMerchant: renameMerchant.trim() || null,
				requireReview,
				isActive
			};

			if (editingRule) {
				const updated = await updateCategorizationRule(editingRule.id, basePayload);
				await onSaved?.({ rule: updated, appliedCount: 0 });
			} else {
				const created = await createCategorizationRule({
					...basePayload,
					applyNow
				});
				await onSaved?.(created);
			}
			onClose();
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to save categorization rule.';
		} finally {
			saving = false;
		}
	}
</script>

{#if isOpen}
	<div
		data-testid="rule-modal"
		role="dialog"
		aria-modal="true"
		aria-labelledby="rule-modal-title"
		class="fixed inset-0 z-50 flex items-center justify-center p-4"
	>
		<button
			type="button"
			tabindex="-1"
			aria-label="Close rule modal"
			onclick={() => {
				if (!saving) onClose();
			}}
			class="fixed inset-0 bg-black/50"
		></button>

		<form
			onsubmit={handleSubmit}
			class="relative z-10 w-full max-w-lg rounded-2xl border border-border bg-surface p-5 shadow-2xl space-y-4 max-h-[90dvh] overflow-y-auto"
		>
			<div class="flex items-center justify-between gap-3">
				<div>
					<h3 id="rule-modal-title" class="text-base font-bold text-ink-primary">
						{editingRule ? 'Edit Categorization Rule' : 'New Categorization Rule'}
					</h3>
					<p class="text-xs text-ink-secondary mt-0.5">
						Automatically categorize matching bank-synced transactions.
					</p>
				</div>
				<button
					type="button"
					onclick={() => {
						if (!saving) onClose();
					}}
					class="p-1.5 rounded-lg text-ink-secondary hover:text-ink-primary hover:bg-surface-hover"
				>
					✕
				</button>
			</div>

			<div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
				<div>
					<label for="rule-match-type" class="block text-xs font-medium text-ink-secondary mb-1">
						Match Type
					</label>
					<select
						id="rule-match-type"
						bind:value={matchType}
						class="w-full rounded-lg border border-border bg-canvas px-2.5 py-2 text-xs text-ink-primary focus:border-accent-pop focus:outline-none"
					>
						<option value="CONTAINS">Contains</option>
						<option value="STARTS_WITH">Starts with</option>
						<option value="ENDS_WITH">Ends with</option>
						<option value="EXACT">Exact match</option>
						<option value="REGEX">Regex</option>
					</select>
				</div>

				<div class="sm:col-span-2">
					<label for="rule-pattern" class="block text-xs font-medium text-ink-secondary mb-1">
						Merchant Pattern
					</label>
					<input
						id="rule-pattern"
						type="text"
						required
						placeholder="e.g. Trader Joe's"
						bind:value={merchantPattern}
						class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
					/>
				</div>
			</div>

			<div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
				<div>
					<label for="rule-account" class="block text-xs font-medium text-ink-secondary mb-1">
						Account (optional)
					</label>
					<select
						id="rule-account"
						bind:value={accountId}
						class="w-full rounded-lg border border-border bg-canvas px-2.5 py-2 text-xs text-ink-primary focus:border-accent-pop focus:outline-none"
					>
						<option value="">Any budget account</option>
						{#each budgetAccounts as acct (acct.id)}
							<option value={acct.id}>{acct.name}</option>
						{/each}
					</select>
				</div>

				<div>
					<label for="rule-direction" class="block text-xs font-medium text-ink-secondary mb-1">
						Direction
					</label>
					<select
						id="rule-direction"
						bind:value={direction}
						class="w-full rounded-lg border border-border bg-canvas px-2.5 py-2 text-xs text-ink-primary focus:border-accent-pop focus:outline-none"
					>
						<option value="ANY">Any (Expense or Inflow)</option>
						<option value="EXPENSE">Expense only (-)</option>
						<option value="INFLOW">Inflow / Credit only (+)</option>
					</select>
				</div>
			</div>

			<div class="grid grid-cols-2 gap-3">
				<div>
					<label for="rule-min-amt" class="block text-xs font-medium text-ink-secondary mb-1">
						Min Amount ($)
					</label>
					<input
						id="rule-min-amt"
						type="number"
						step="0.01"
						min="0"
						placeholder="Optional"
						bind:value={minAmount}
						class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-xs font-mono text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
					/>
				</div>
				<div>
					<label for="rule-max-amt" class="block text-xs font-medium text-ink-secondary mb-1">
						Max Amount ($)
					</label>
					<input
						id="rule-max-amt"
						type="number"
						step="0.01"
						min="0"
						placeholder="Optional"
						bind:value={maxAmount}
						class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-xs font-mono text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
					/>
				</div>
			</div>

			<div class="rounded-xl border border-border bg-subtle/60 p-3 space-y-3">
				<div class="flex items-center justify-between gap-2">
					<span class="text-xs font-semibold text-ink-primary">Then Assign To</span>
					<div class="inline-flex rounded-lg border border-border bg-surface p-0.5 text-[11px]">
						<button
							type="button"
							onclick={() => (targetKind = 'category')}
							class="px-2.5 py-1 rounded-md font-medium {targetKind === 'category'
								? 'bg-accent-primary text-accent-primary-text'
								: 'text-ink-secondary hover:text-ink-primary'}"
						>
							Category
						</button>
						<button
							type="button"
							onclick={() => (targetKind = 'income')}
							class="px-2.5 py-1 rounded-md font-medium {targetKind === 'income'
								? 'bg-accent-primary text-accent-primary-text'
								: 'text-ink-secondary hover:text-ink-primary'}"
						>
							Planned Income
						</button>
					</div>
				</div>

				{#if targetKind === 'category'}
					<div class="space-y-1.5">
						<div class="flex items-center justify-between gap-2">
							<label for="rule-category-search" class="block text-xs font-medium text-ink-secondary">
								Category
							</label>
							<span class="text-[11px] font-semibold text-ink-primary truncate max-w-[200px]">
								{selectedCategoryLabel}
							</span>
						</div>

						<input
							id="rule-category-search"
							type="search"
							aria-label="Search categories"
							placeholder="Search categories..."
							bind:value={categoryFilter}
							class="w-full rounded-lg border border-border bg-canvas px-3 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
						/>

						<div
							data-testid="rule-category-picker"
							class="max-h-44 overflow-y-auto overscroll-contain-scroll rounded-lg border border-border bg-canvas p-2.5 space-y-2"
						>
							{#if filteredGroupedCategories.length === 0}
								<p class="text-[11px] text-ink-muted py-1 text-center">No matching categories</p>
							{:else}
								{#each filteredGroupedCategories as [grp, cats] (grp)}
									<div class="space-y-1">
										<p class="text-[10px] font-semibold uppercase tracking-wider text-ink-muted">
											{grp}
										</p>
										<div class="flex flex-wrap gap-1.5">
											{#each cats as cat (cat.id)}
												<button
													type="button"
													data-testid="rule-category-option"
													onclick={() => (categoryId = cat.id)}
													class="rounded-full border px-2.5 py-0.5 text-[11px] font-medium transition-colors {categoryId === cat.id
														? 'border-accent-pop bg-accent-tint text-accent-pop font-semibold'
														: 'border-border bg-surface text-ink-primary hover:bg-surface-hover'}"
												>
													{cat.name}
												</button>
											{/each}
										</div>
									</div>
								{/each}
							{/if}
						</div>
					</div>
				{:else}
					<div>
						<label for="rule-income-name" class="block text-xs font-medium text-ink-secondary mb-1">
							Planned Income Name (matched across months)
						</label>
						<input
							id="rule-income-name"
							type="text"
							list="rule-income-suggestions"
							placeholder="e.g. Primary Paycheck"
							bind:value={plannedIncomeName}
							required
							class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
						/>
						<datalist id="rule-income-suggestions">
							{#each incomes as inc (inc.id)}
								<option value={inc.name}></option>
							{/each}
						</datalist>
					</div>
				{/if}

				<div>
					<label for="rule-rename" class="block text-xs font-medium text-ink-secondary mb-1">
						Rename Merchant (optional)
					</label>
					<input
						id="rule-rename"
						type="text"
						placeholder="Leave blank to keep normalized name"
						bind:value={renameMerchant}
						class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
					/>
				</div>
			</div>

			<div class="space-y-2.5">
				<span class="block text-xs font-semibold text-ink-primary">
					When this rule matches a synced transaction:
				</span>
				<div class="grid grid-cols-1 sm:grid-cols-2 gap-2">
					<button
						type="button"
						data-testid="rule-review-keep-btn"
						onclick={() => (requireReview = true)}
						class="text-left rounded-xl border p-2.5 transition-colors {requireReview
							? 'border-accent-pop bg-accent-tint/70'
							: 'border-border bg-canvas hover:bg-surface-hover'}"
					>
						<span class="block text-xs font-semibold text-ink-primary">
							Keep in To Review for approval
						</span>
						<span class="block text-[11px] text-ink-secondary mt-0.5">
							Assigns the category, then waits for your 1-click Approve.
						</span>
					</button>

					<button
						type="button"
						data-testid="rule-review-auto-btn"
						onclick={() => (requireReview = false)}
						class="text-left rounded-xl border p-2.5 transition-colors {!requireReview
							? 'border-accent-pop bg-accent-tint/70'
							: 'border-border bg-canvas hover:bg-surface-hover'}"
					>
						<span class="block text-xs font-semibold text-ink-primary">
							Auto-approve (skip To Review)
						</span>
						<span class="block text-[11px] text-ink-secondary mt-0.5">
							Categorizes posted transactions silently with no review needed.
						</span>
					</button>
				</div>

				{#if editingRule}
					<label class="flex items-center gap-2 text-xs text-ink-primary cursor-pointer pt-1">
						<input type="checkbox" bind:checked={isActive} class="rounded border-border" />
						<span class="font-medium">Rule enabled</span>
					</label>
				{:else}
					<label class="flex items-center gap-2 text-xs text-ink-primary cursor-pointer pt-1">
						<input type="checkbox" bind:checked={applyNow} class="rounded border-border" />
						<span class="font-medium">Also apply to existing uncategorized transactions now</span>
					</label>
				{/if}
			</div>

			{#if errorMsg}
				<p role="alert" class="text-xs text-status-danger bg-status-danger-bg border border-status-danger/30 rounded-lg px-3 py-2">
					{errorMsg}
				</p>
			{/if}

			<div class="flex items-center justify-end gap-2 pt-1">
				<button
					type="button"
					disabled={saving}
					onclick={onClose}
					class="px-3.5 py-2 rounded-lg border border-border bg-surface text-xs font-medium text-ink-secondary hover:text-ink-primary"
				>
					Cancel
				</button>
				<button
					type="submit"
					data-testid="rule-modal-submit"
					disabled={saving}
					class="px-4 py-2 rounded-lg bg-accent-primary text-accent-primary-text text-xs font-semibold hover:opacity-90 disabled:opacity-50"
				>
					{saving ? 'Saving...' : editingRule ? 'Save Rule' : 'Create Rule'}
				</button>
			</div>
		</form>
	</div>
{/if}
