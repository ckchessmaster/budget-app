<script module lang="ts">
	export type CatchUpMode = 'intro' | 'stack';
</script>

<script lang="ts">
	import type { CategoryBudget } from '$lib/types/budget';
	import type { CategorizationSuggestion } from '$lib/types/categorization';
	import type { AccountItem, TransactionItemModel } from '$lib/types/transaction';
	import {
		getReviewBadgeKind,
		suggestCategoriesForTransaction
	} from '$lib/utils/transactionReview';

	interface Props {
		isOpen: boolean;
		initialMode?: CatchUpMode;
		items: TransactionItemModel[];
		allTransactions?: TransactionItemModel[];
		accounts: AccountItem[];
		categories: CategoryBudget[];
		suggestionsByTxId?: Record<string, CategorizationSuggestion[]>;
		onCategorizeTransaction: (txId: string, categoryId: string) => Promise<void>;
		onAssignTransactionToIncome?: (txId: string, incomeId: string) => Promise<void>;
		onSplitTransaction?: (txId: string) => void;
		onClose: () => void;
	}

	let {
		isOpen,
		initialMode = 'intro',
		items,
		allTransactions = [],
		accounts,
		categories,
		suggestionsByTxId = {},
		onCategorizeTransaction,
		onAssignTransactionToIncome,
		onSplitTransaction,
		onClose
	}: Props = $props();

	let mode = $state<CatchUpMode>('intro');
	let handledIds = $state<string[]>([]);
	let currentIndex = $state(0);
	let showAllCategories = $state(false);
	let categorySearch = $state('');
	let isSaving = $state(false);
	let errorMsg = $state<string | null>(null);

	let wasOpen = false;
	$effect(() => {
		if (isOpen && !wasOpen) {
			mode = initialMode;
			handledIds = [];
			currentIndex = 0;
			showAllCategories = false;
			categorySearch = '';
			errorMsg = null;
		}
		wasOpen = isOpen;
	});

	$effect(() => {
		if (!isOpen) return;

		function handleKeyDown(e: KeyboardEvent) {
			if (e.key === 'Escape' && !isSaving) {
				e.preventDefault();
				onClose();
			}
		}

		window.addEventListener('keydown', handleKeyDown);
		return () => {
			window.removeEventListener('keydown', handleKeyDown);
		};
	});

	let accountById = $derived(new Map(accounts.map((a) => [a.id, a])));
	let remainingItems = $derived(items.filter((tx) => !handledIds.includes(tx.id)));
	let safeIndex = $derived(
		remainingItems.length > 0 ? Math.min(currentIndex, remainingItems.length - 1) : 0
	);
	let currentItem = $derived(remainingItems[safeIndex] ?? null);

	interface SuggestedOption {
		key: string;
		name: string;
		reason?: string;
		categoryId?: string;
		plannedIncomeId?: string;
	}

	let suggestedOptions = $derived.by((): SuggestedOption[] => {
		if (!currentItem) return [];
		const backendList = suggestionsByTxId[currentItem.id];
		if (backendList && backendList.length > 0) {
			return backendList
				.filter(
					(s) =>
						(s.targetType === 'CATEGORY' && Boolean(s.categoryId)) ||
						(s.targetType === 'PLANNED_INCOME' &&
							Boolean(s.plannedIncomeId) &&
							Boolean(onAssignTransactionToIncome))
				)
				.map((s) => ({
					key: s.categoryId ?? s.plannedIncomeId ?? s.name,
					name: s.name,
					reason: s.reason,
					categoryId: s.categoryId ?? undefined,
					plannedIncomeId: s.plannedIncomeId ?? undefined
				}));
		}
		return suggestCategoriesForTransaction(currentItem, categories, allTransactions, 4).map((c) => ({
			key: c.id,
			name: c.name,
			categoryId: c.id
		}));
	});

	let groupedCategories = $derived.by(() => {
		const groups = new Map<string, CategoryBudget[]>();
		for (const cat of categories) {
			const list = groups.get(cat.groupName) ?? [];
			list.push(cat);
			groups.set(cat.groupName, list);
		}
		return Array.from(groups.entries());
	});

	let filteredCategoryGroups = $derived.by(() => {
		const q = categorySearch.trim().toLowerCase();
		if (!q) return groupedCategories;
		return groupedCategories
			.map(
				([groupName, cats]) =>
					[
						groupName,
						cats.filter(
							(c) =>
								c.name.toLowerCase().includes(q) ||
								groupName.toLowerCase().includes(q)
						)
					] as [string, CategoryBudget[]]
			)
			.filter(([, cats]) => cats.length > 0);
	});

	function formatShortDate(isoDate: string): string {
		const [y, m, d] = isoDate.slice(0, 10).split('-').map(Number);
		if (!y || !m || !d) return isoDate;
		return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString('en-US', {
			month: 'short',
			day: 'numeric',
			timeZone: 'UTC'
		});
	}

	function formatFullDate(isoDate: string): string {
		const [y, m, d] = isoDate.slice(0, 10).split('-').map(Number);
		if (!y || !m || !d) return isoDate;
		return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString('en-US', {
			month: 'short',
			day: 'numeric',
			year: 'numeric',
			timeZone: 'UTC'
		});
	}

	function formatCurrency(val: number): string {
		const abs = Math.abs(val).toLocaleString('en-US', {
			minimumFractionDigits: 2,
			maximumFractionDigits: 2
		});
		return val < 0 ? `-$${abs}` : `+$${abs}`;
	}

	function formatRemaining(val: number): string {
		const abs = Math.abs(val).toLocaleString('en-US', {
			minimumFractionDigits: 2,
			maximumFractionDigits: 2
		});
		return val < 0 ? `-$${abs}` : `$${abs}`;
	}

	async function handleAssignOption(opt: { categoryId?: string; plannedIncomeId?: string }) {
		if (!currentItem || isSaving) return;
		const txId = currentItem.id;
		isSaving = true;
		errorMsg = null;
		try {
			if (opt.plannedIncomeId && onAssignTransactionToIncome) {
				await onAssignTransactionToIncome(txId, opt.plannedIncomeId);
			} else if (opt.categoryId) {
				await onCategorizeTransaction(txId, opt.categoryId);
			}
			const nextHandled = [...handledIds, txId];
			handledIds = nextHandled;
			showAllCategories = false;
			categorySearch = '';
			const nextRemaining = items.filter((tx) => !nextHandled.includes(tx.id));
			if (nextRemaining.length === 0) {
				onClose();
			} else if (currentIndex >= nextRemaining.length) {
				currentIndex = 0;
			}
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to categorize transaction.';
		} finally {
			isSaving = false;
		}
	}

	async function handleAssignCategory(categoryId: string) {
		return handleAssignOption({ categoryId });
	}

	function handleSkipItem() {
		if (isSaving || remainingItems.length <= 1) return;
		currentIndex = (safeIndex + 1) % remainingItems.length;
		showAllCategories = false;
		categorySearch = '';
		errorMsg = null;
	}

	let stackProgressPct = $derived(
		remainingItems.length > 0 ? ((safeIndex + 1) / remainingItems.length) * 100 : 100
	);
</script>

{#if isOpen}
	<div
		data-testid="catch-up-modal"
		role="dialog"
		aria-modal="true"
		aria-labelledby="catchup-modal-title"
		class="fixed inset-0 z-50 flex items-center justify-center p-4"
	>
		<button
			type="button"
			tabindex="-1"
			aria-label="Close catch up modal"
			data-testid="catchup-backdrop"
			onclick={() => {
				if (!isSaving) onClose();
			}}
			class="fixed inset-0 bg-black/50 transition-opacity"
		></button>

		<div class="relative z-10 w-full max-w-lg rounded-2xl border border-border bg-surface shadow-2xl overflow-hidden flex flex-col max-h-[90dvh]">
			{#if mode === 'intro'}
				<div class="px-5 py-4 border-b border-border bg-subtle flex items-start justify-between gap-3 shrink-0">
					<div>
						<h2 id="catchup-modal-title" class="text-base font-bold text-ink-primary">
							Time to catch up
						</h2>
						<p class="text-xs text-ink-secondary mt-0.5">
							{remainingItems.length}
							{remainingItems.length === 1 ? 'transaction needs' : 'transactions need'} a category.
						</p>
					</div>

					<button
						type="button"
						aria-label="Close modal"
						onclick={() => {
							if (!isSaving) onClose();
						}}
						class="p-1.5 rounded-lg text-ink-secondary hover:text-ink-primary hover:bg-surface-hover transition-colors"
					>
						<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
							<path d="M18 6 6 18" />
							<path d="m6 6 12 12" />
						</svg>
					</button>
				</div>

				<div class="flex-1 min-h-0 overflow-y-auto overscroll-contain-scroll divide-y divide-border">
					{#if remainingItems.length === 0}
						<div class="p-6 text-center text-sm text-ink-secondary">
							You're all caught up!
						</div>
					{:else}
						{#each remainingItems as tx (tx.id)}
							{@const acct = accountById.get(tx.accountId)}
							{@const badgeKind = getReviewBadgeKind(tx, accountById)}
							{@const displayMerchant = tx.merchantClean?.trim() || tx.merchantRaw}
							<div
								data-testid="catchup-intro-item"
								class="px-5 py-3 flex items-center justify-between gap-3"
							>
								<div class="min-w-0 flex-1">
									<div class="flex items-center gap-2 flex-wrap">
										<span class="text-xs font-semibold text-ink-primary truncate">
											{displayMerchant}
										</span>
										{#if badgeKind === 'pending'}
											<span
												data-testid="catchup-badge-pending"
												class="inline-flex items-center text-[10px] font-medium bg-status-warn-bg text-status-warn px-1.5 py-0.5 rounded"
											>
												Pending
											</span>
										{:else if badgeKind === 'new'}
											<span
												data-testid="catchup-badge-new"
												class="inline-flex items-center text-[10px] font-medium bg-accent-tint text-accent-pop px-1.5 py-0.5 rounded"
											>
												New
											</span>
										{/if}
									</div>
									<div class="flex items-center gap-1.5 mt-0.5 text-[11px] text-ink-secondary">
										<span aria-label={tx.transactionDate}>{formatShortDate(tx.transactionDate)}</span>
										<span aria-hidden="true">·</span>
										<span class="truncate">{tx.accountName || acct?.name || 'Account'}</span>
									</div>
								</div>

								<span
									class="text-xs font-mono font-bold shrink-0 {tx.amount > 0
										? 'text-status-good'
										: 'text-ink-primary'}"
								>
									{formatCurrency(tx.amount)}
								</span>
							</div>
						{/each}
					{/if}
				</div>

				<div class="px-5 py-3.5 border-t border-border bg-subtle flex items-center justify-end gap-2 shrink-0">
					<button
						type="button"
						data-testid="catchup-skip-for-now-btn"
						onclick={onClose}
						class="rounded-lg border border-border bg-surface hover:bg-surface-hover px-3.5 py-2 text-xs font-semibold text-ink-secondary hover:text-ink-primary transition-colors"
					>
						Skip for Now
					</button>
					<button
						type="button"
						data-testid="catchup-now-btn"
						disabled={remainingItems.length === 0}
						onclick={() => {
							mode = 'stack';
							errorMsg = null;
						}}
						class="rounded-lg bg-accent-pop text-accent-primary-text px-4 py-2 text-xs font-semibold hover:opacity-90 disabled:opacity-50 transition-opacity"
					>
						Catch Up Now
					</button>
				</div>
			{:else}
				<div class="px-5 pt-4 pb-3 border-b border-border bg-subtle shrink-0 space-y-2.5">
					<div class="flex items-center justify-between gap-3">
						<h2
							id="catchup-modal-title"
							data-testid="catchup-stack-progress"
							class="text-sm font-bold text-ink-primary"
						>
							{#if remainingItems.length > 0}
								Catch Up · {safeIndex + 1} of {remainingItems.length}
							{:else}
								Catch Up
							{/if}
						</h2>

						<button
							type="button"
							aria-label="Close modal"
							disabled={isSaving}
							onclick={onClose}
							class="p-1.5 rounded-lg text-ink-secondary hover:text-ink-primary hover:bg-surface-hover disabled:opacity-50 transition-colors"
						>
							<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
								<path d="M18 6 6 18" />
								<path d="m6 6 12 12" />
							</svg>
						</button>
					</div>

					<div class="h-1.5 w-full rounded-full bg-border overflow-hidden">
						<div
							class="h-full rounded-full bg-accent-pop transition-all duration-200"
							style="width: {stackProgressPct}%"
						></div>
					</div>
				</div>

				<div class="flex-1 min-h-0 overflow-y-auto overscroll-contain-scroll p-5 space-y-4">
					{#if currentItem}
						{@const acct = accountById.get(currentItem.accountId)}
						{@const badgeKind = getReviewBadgeKind(currentItem, accountById)}
						{@const displayMerchant = currentItem.merchantClean?.trim() || currentItem.merchantRaw}

						<div
							data-testid="catchup-stack-card"
							class="rounded-xl border border-border bg-canvas p-4 space-y-3"
						>
							<div class="flex items-center justify-between gap-2">
								<div class="flex items-center gap-2">
									{#if badgeKind === 'pending'}
										<span
											data-testid="catchup-badge-pending"
											class="inline-flex items-center text-[11px] font-medium bg-status-warn-bg text-status-warn px-2 py-0.5 rounded"
										>
											Pending
										</span>
									{:else if badgeKind === 'new'}
										<span
											data-testid="catchup-badge-new"
											class="inline-flex items-center text-[11px] font-medium bg-accent-tint text-accent-pop px-2 py-0.5 rounded"
										>
											New
										</span>
									{/if}
									<span
										aria-label={currentItem.transactionDate}
										class="text-xs text-ink-secondary"
									>
										{formatFullDate(currentItem.transactionDate)}
									</span>
								</div>

								<span class="text-xs text-ink-secondary truncate">
									{currentItem.accountName || acct?.name || 'Account'}
								</span>
							</div>

							<div class="flex items-baseline justify-between gap-3">
								<div class="min-w-0">
									<p class="text-base font-bold text-ink-primary truncate">
										{displayMerchant}
									</p>
									{#if currentItem.userNotes}
										<p class="text-xs text-ink-muted mt-0.5 truncate">
											{currentItem.userNotes}
										</p>
									{/if}
								</div>

								<span
									class="text-xl font-mono font-bold shrink-0 {currentItem.amount > 0
										? 'text-status-good'
										: 'text-ink-primary'}"
								>
									{formatCurrency(currentItem.amount)}
								</span>
							</div>
						</div>

						{#if errorMsg}
							<div
								role="alert"
								class="rounded-lg border border-status-danger bg-status-danger-bg px-3 py-2 text-xs text-status-danger"
							>
								{errorMsg}
							</div>
						{/if}

						<div class="space-y-2.5">
							<div class="flex items-center justify-between">
								<span class="text-xs font-semibold text-ink-secondary">Sort into:</span>
								{#if isSaving}
									<span class="text-[11px] text-accent-pop font-medium">Saving...</span>
								{/if}
							</div>

							<div class="flex flex-wrap gap-2">
								{#each suggestedOptions as opt (opt.key)}
									<button
										type="button"
										data-testid="catchup-category-chip"
										disabled={isSaving}
										onclick={() => void handleAssignOption(opt)}
										class="inline-flex items-center gap-1 rounded-full border border-border bg-subtle hover:bg-accent-tint hover:border-accent-pop hover:text-accent-pop px-3 py-1.5 text-xs font-medium text-ink-primary disabled:opacity-50 transition-colors"
									>
										<span>{opt.name}</span>
										{#if opt.reason}
											<span class="text-[10px] text-ink-secondary">· {opt.reason}</span>
										{/if}
									</button>
								{/each}

								<button
									type="button"
									data-testid="catchup-more-btn"
									disabled={isSaving}
									aria-expanded={showAllCategories}
									onclick={() => (showAllCategories = !showAllCategories)}
									class="rounded-full border border-border bg-surface hover:bg-surface-hover px-3 py-1.5 text-xs font-semibold text-accent-pop disabled:opacity-50 transition-colors"
								>
									{showAllCategories ? 'Less' : 'More...'}
								</button>

								{#if onSplitTransaction && currentItem && !currentItem.isTransfer}
									<button
										type="button"
										data-testid="catchup-split-btn"
										disabled={isSaving}
										onclick={() => {
											const id = currentItem.id;
											onClose();
											onSplitTransaction(id);
										}}
										class="rounded-full border border-border bg-surface hover:bg-surface-hover px-3 py-1.5 text-xs font-semibold text-ink-secondary hover:text-ink-primary disabled:opacity-50 transition-colors"
									>
										Split
									</button>
								{/if}
							</div>
						</div>

						{#if showAllCategories}
							<div
								data-testid="catchup-all-categories"
								class="rounded-xl border border-border bg-canvas p-3 space-y-2.5"
							>
								<input
									type="text"
									bind:value={categorySearch}
									aria-label="Search categories"
									placeholder="Search all categories..."
									class="w-full rounded-lg border border-border bg-surface px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
								/>

								<div class="max-h-48 overflow-y-auto overscroll-contain-scroll space-y-2.5 pr-1">
									{#if filteredCategoryGroups.length === 0}
										<p class="text-xs text-ink-muted py-2 text-center">
											No categories match "{categorySearch}".
										</p>
									{:else}
										{#each filteredCategoryGroups as [groupName, cats] (groupName)}
											<div class="space-y-1">
												<div class="text-[10px] font-bold uppercase tracking-wider text-ink-muted px-1">
													{groupName}
												</div>
												<div class="space-y-1">
													{#each cats as cat (cat.id)}
														<button
															type="button"
															data-testid="catchup-all-category-option"
															disabled={isSaving}
															onclick={() => void handleAssignCategory(cat.id)}
															class="w-full flex items-center justify-between rounded-lg border border-transparent hover:border-border hover:bg-surface px-2.5 py-1.5 text-left text-xs text-ink-primary disabled:opacity-50 transition-colors"
														>
															<span class="font-medium truncate">{cat.name}</span>
															<span class="font-mono text-[11px] text-ink-secondary shrink-0">
																{formatRemaining(cat.remaining)} left
															</span>
														</button>
													{/each}
												</div>
											</div>
										{/each}
									{/if}
								</div>
							</div>
						{/if}
					{:else}
						<div class="py-8 text-center space-y-3">
							<p class="text-sm font-semibold text-ink-primary">All caught up!</p>
							<p class="text-xs text-ink-secondary">
								Every transaction in your review queue has a category.
							</p>
							<button
								type="button"
								onclick={onClose}
								class="rounded-lg bg-accent-pop text-accent-primary-text px-4 py-2 text-xs font-semibold hover:opacity-90 transition-opacity"
							>
								Done
							</button>
						</div>
					{/if}
				</div>

				{#if currentItem}
					<div class="px-5 py-3.5 border-t border-border bg-subtle flex items-center justify-between gap-2 shrink-0">
						<button
							type="button"
							data-testid="catchup-finish-later-btn"
							disabled={isSaving}
							onclick={onClose}
							class="rounded-lg px-3 py-1.5 text-xs font-medium text-ink-secondary hover:text-ink-primary hover:bg-surface-hover disabled:opacity-50 transition-colors"
						>
							Finish later
						</button>

						<button
							type="button"
							data-testid="catchup-skip-btn"
							disabled={isSaving || remainingItems.length <= 1}
							onclick={handleSkipItem}
							class="rounded-lg border border-border bg-surface hover:bg-surface-hover px-3.5 py-1.5 text-xs font-semibold text-ink-primary disabled:opacity-40 transition-colors"
						>
							Skip &rarr;
						</button>
					</div>
				{/if}
			{/if}
		</div>
	</div>
{/if}
