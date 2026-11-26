<script lang="ts">
	import type { CategoryBudget, PlannedIncome } from '$lib/types/budget';
	import type { CategorizationSuggestion } from '$lib/types/categorization';
	import type {
		AccountItem,
		ReplaceSplitsPayload,
		TransactionItemModel,
		UpdateTransactionPayload
	} from '$lib/types/transaction';

	interface Props {
		transaction: TransactionItemModel;
		account: AccountItem | null;
		categories: CategoryBudget[];
		incomes?: PlannedIncome[];
		suggestions?: CategorizationSuggestion[];
		variant?: 'panel' | 'modal';
		initialSplitMode?: boolean;
		onClose: () => void;
		onUpdateTransaction?: (txId: string, payload: UpdateTransactionPayload) => Promise<void>;
		onReplaceSplits?: (txId: string, payload: ReplaceSplitsPayload) => Promise<void>;
		onApproveReview?: (txId: string) => Promise<void>;
		onCreateRule?: (tx: TransactionItemModel) => void;
		onDeleteTransaction?: (txId: string) => Promise<void>;
	}

	let {
		transaction,
		account,
		categories,
		incomes = [],
		suggestions = [],
		variant = 'panel',
		initialSplitMode = false,
		onClose,
		onUpdateTransaction,
		onReplaceSplits,
		onApproveReview,
		onCreateRule,
		onDeleteTransaction
	}: Props = $props();

	interface DraftSplitRow {
		amount: string;
		target: string;
		notes: string;
	}

	let editMerchant = $state('');
	let editNotes = $state('');
	let editCategoryId = $state('');
	let categoryFilter = $state('');
	let showIncomeInPicker = $state(false);
	let saving = $state(false);
	let deleting = $state(false);
	let confirmDelete = $state(false);
	let errorMsg = $state<string | null>(null);
	let lastHydratedKey = $state('');

	let isEditingSplits = $state(false);
	let splitRows = $state<DraftSplitRow[]>([]);
	let savingSplits = $state(false);
	let splitError = $state<string | null>(null);

	let isQueued = $derived(
		transaction.origin === 'MANUAL_OFFLINE' && transaction.syncStatus === 'QUEUED'
	);
	let hasSplits = $derived(Boolean(transaction.splits && transaction.splits.length > 0));
	let isOffBudget = $derived(account ? !account.isOnBudget : false);
	let canCategorize = $derived(!isQueued && !transaction.isTransfer && !isOffBudget && !hasSplits);
	let canSplit = $derived(
		!isQueued && !transaction.isTransfer && !isOffBudget && transaction.amount !== 0
	);
	let displayMerchant = $derived(transaction.merchantClean?.trim() || transaction.merchantRaw);
	let showOriginalMerchant = $derived(
		Boolean(
			transaction.merchantClean?.trim() &&
				transaction.merchantClean.trim() !== transaction.merchantRaw
		)
	);

	let parentAbsAmount = $derived(Math.round(Math.abs(transaction.amount) * 100) / 100);
	let parentSign = $derived(transaction.amount < 0 ? -1 : 1);

	let splitAllocated = $derived.by(() => {
		let sum = 0;
		for (const row of splitRows) {
			const v = Number(row.amount);
			if (Number.isFinite(v) && v > 0) sum += v;
		}
		return Math.round(sum * 100) / 100;
	});
	let splitRemaining = $derived(Math.round((parentAbsAmount - splitAllocated) * 100) / 100);

	let categoryById = $derived(new Map(categories.map((c) => [c.id, c])));
	let incomeById = $derived(new Map(incomes.map((i) => [i.id, i])));
	let groupedCategories = $derived.by(() => {
		const groups = new Map<string, CategoryBudget[]>();
		for (const cat of categories) {
			const list = groups.get(cat.groupName) ?? [];
			list.push(cat);
			groups.set(cat.groupName, list);
		}
		return Array.from(groups.entries());
	});

	let filteredGroups = $derived.by(() => {
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

	let filteredIncomes = $derived.by(() => {
		const q = categoryFilter.trim().toLowerCase();
		if (!q) return incomes;
		return incomes.filter((inc) => inc.name.toLowerCase().includes(q) || 'income'.includes(q));
	});

	let filteredSuggestions = $derived.by(() => {
		const q = categoryFilter.trim().toLowerCase();
		if (!q) return suggestions;
		return suggestions.filter(
			(s) =>
				s.name.toLowerCase().includes(q) ||
				(s.groupName ?? '').toLowerCase().includes(q)
		);
	});

	let selectedTargetLabel = $derived.by(() => {
		if (!editCategoryId) return 'Uncategorized';
		if (editCategoryId.startsWith('income:')) {
			const inc = incomeById.get(editCategoryId.slice('income:'.length));
			return inc ? `Income: ${inc.name}` : 'Income';
		}
		const cat = categoryById.get(editCategoryId);
		return cat ? cat.name : 'Selected';
	});

	function buildInitialSplitRows(tx: TransactionItemModel): DraftSplitRow[] {
		if (tx.splits && tx.splits.length > 0) {
			return tx.splits.map((s) => ({
				amount: Math.abs(s.amount).toFixed(2),
				target: s.plannedIncomeId ? `income:${s.plannedIncomeId}` : (s.categoryId ?? ''),
				notes: s.userNotes ?? ''
			}));
		}
		const initialTarget = tx.plannedIncomeId
			? `income:${tx.plannedIncomeId}`
			: (tx.categoryId ?? '');
		return [
			{ amount: '', target: initialTarget, notes: '' },
			{ amount: '', target: '', notes: '' }
		];
	}

	function openSplitEditor() {
		splitRows = buildInitialSplitRows(transaction);
		splitError = null;
		isEditingSplits = true;
	}

	$effect(() => {
		const splitSig = (transaction.splits ?? [])
			.map((s) => `${s.id}:${s.amount}:${s.categoryId ?? ''}:${s.plannedIncomeId ?? ''}`)
			.join(',');
		const key = `${transaction.id}|${transaction.merchantClean ?? ''}|${transaction.userNotes ?? ''}|${transaction.categoryId ?? ''}|${transaction.plannedIncomeId ?? ''}|${splitSig}|${initialSplitMode ? 'split' : 'view'}`;
		if (key !== lastHydratedKey) {
			lastHydratedKey = key;
			editMerchant = transaction.merchantClean?.trim() || transaction.merchantRaw;
			editNotes = transaction.userNotes ?? '';
			editCategoryId = transaction.plannedIncomeId
				? `income:${transaction.plannedIncomeId}`
				: transaction.categoryId ?? '';
			categoryFilter = '';
			showIncomeInPicker = false;
			confirmDelete = false;
			errorMsg = null;
			splitError = null;
			if (initialSplitMode && canSplit && onReplaceSplits) {
				splitRows = buildInitialSplitRows(transaction);
				isEditingSplits = true;
			} else {
				isEditingSplits = false;
			}
		}
	});

	function formatCurrency(val: number): string {
		const abs = Math.abs(val).toLocaleString('en-US', {
			minimumFractionDigits: 2,
			maximumFractionDigits: 2
		});
		if (val > 0) return `+$${abs}`;
		if (val < 0) return `-$${abs}`;
		return `$${abs}`;
	}

	function formatDate(isoDate: string): string {
		const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(isoDate);
		if (!m) return isoDate;
		const d = new Date(Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3])));
		if (Number.isNaN(d.getTime())) return isoDate;
		return d.toLocaleDateString('en-US', {
			month: 'short',
			day: 'numeric',
			year: 'numeric',
			timeZone: 'UTC'
		});
	}

	function originLabel(origin: TransactionItemModel['origin']): string {
		if (origin === 'BANK_FEED') return 'Bank Sync';
		if (origin === 'RECURRING') return 'Recurring';
		return 'Manual';
	}

	async function handleSave(e: SubmitEvent) {
		e.preventDefault();
		const trimmed = editMerchant.trim();
		if (!trimmed || !onUpdateTransaction) return;

		saving = true;
		errorMsg = null;
		try {
			const payload: UpdateTransactionPayload = {
				merchantClean: trimmed,
				userNotes: editNotes.trim()
			};
			if (canCategorize) {
				if (editCategoryId.startsWith('income:')) {
					payload.plannedIncomeId = editCategoryId.slice('income:'.length);
					payload.clearCategory = true;
				} else if (editCategoryId) {
					payload.categoryId = editCategoryId;
					payload.clearPlannedIncome = true;
				} else {
					payload.clearCategory = true;
					payload.clearPlannedIncome = true;
				}
			}
			await onUpdateTransaction(transaction.id, payload);
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to update transaction.';
		} finally {
			saving = false;
		}
	}

	async function handleApprove() {
		saving = true;
		errorMsg = null;
		try {
			if (onApproveReview) {
				await onApproveReview(transaction.id);
			} else if (onUpdateTransaction) {
				await onUpdateTransaction(transaction.id, { approveReview: true });
			}
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to approve transaction.';
		} finally {
			saving = false;
		}
	}

	async function handleApplySuggestion(sug: CategorizationSuggestion) {
		if (!onUpdateTransaction) return;
		saving = true;
		errorMsg = null;
		try {
			if (sug.targetType === 'PLANNED_INCOME' && sug.plannedIncomeId) {
				editCategoryId = `income:${sug.plannedIncomeId}`;
				await onUpdateTransaction(transaction.id, {
					plannedIncomeId: sug.plannedIncomeId,
					clearCategory: true
				});
			} else if (sug.categoryId) {
				editCategoryId = sug.categoryId;
				await onUpdateTransaction(transaction.id, {
					categoryId: sug.categoryId,
					clearPlannedIncome: true
				});
			}
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to apply suggestion.';
		} finally {
			saving = false;
		}
	}

	async function handleDeleteConfirmed() {
		if (!onDeleteTransaction) return;
		deleting = true;
		errorMsg = null;
		try {
			await onDeleteTransaction(transaction.id);
			onClose();
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to delete transaction.';
		} finally {
			deleting = false;
		}
	}

	function fillSplitRemaining(idx: number) {
		const currentVal = Number(splitRows[idx]?.amount) || 0;
		const nextVal = Math.max(0, Math.round((currentVal + splitRemaining) * 100) / 100);
		splitRows = splitRows.map((r, i) => (i === idx ? { ...r, amount: nextVal.toFixed(2) } : r));
	}

	function addSplitRow() {
		const prefill = splitRemaining > 0.005 ? splitRemaining.toFixed(2) : '';
		splitRows = [...splitRows, { amount: prefill, target: '', notes: '' }];
	}

	function removeSplitRow(idx: number) {
		if (splitRows.length <= 2) return;
		splitRows = splitRows.filter((_, i) => i !== idx);
	}

	async function handleSaveSplits(e: SubmitEvent) {
		e.preventDefault();
		if (!onReplaceSplits) return;
		splitError = null;

		if (splitRows.length < 2) {
			splitError = 'At least 2 splits are required.';
			return;
		}
		const payloadSplits: ReplaceSplitsPayload['splits'] = [];
		for (let i = 0; i < splitRows.length; i++) {
			const row = splitRows[i];
			const amt = Math.round((Number(row.amount) || 0) * 100) / 100;
			if (!Number.isFinite(amt) || amt <= 0) {
				splitError = `Split #${i + 1} amount must be greater than $0.00.`;
				return;
			}
			if (!row.target) {
				splitError = `Please select a category for split #${i + 1}.`;
				return;
			}
			const signedAmt = Math.round(parentSign * amt * 100) / 100;
			if (row.target.startsWith('income:')) {
				payloadSplits.push({
					amount: signedAmt,
					plannedIncomeId: row.target.slice('income:'.length),
					userNotes: row.notes.trim()
				});
			} else {
				payloadSplits.push({
					amount: signedAmt,
					categoryId: row.target,
					userNotes: row.notes.trim()
				});
			}
		}

		if (Math.abs(splitRemaining) >= 0.005) {
			splitError = `Splits must equal ${formatCurrency(transaction.amount)} (remaining: $${Math.abs(splitRemaining).toFixed(2)}).`;
			return;
		}

		savingSplits = true;
		try {
			await onReplaceSplits(transaction.id, { splits: payloadSplits });
			isEditingSplits = false;
		} catch (err) {
			splitError = err instanceof Error ? err.message : 'Failed to save splits.';
		} finally {
			savingSplits = false;
		}
	}

	async function handleUnsplit() {
		if (!onReplaceSplits) return;
		savingSplits = true;
		splitError = null;
		try {
			const first = transaction.splits?.[0];
			await onReplaceSplits(transaction.id, {
				splits: [],
				categoryId: first?.categoryId ?? null,
				plannedIncomeId: first?.plannedIncomeId ?? null
			});
			isEditingSplits = false;
		} catch (err) {
			splitError = err instanceof Error ? err.message : 'Failed to unsplit transaction.';
		} finally {
			savingSplits = false;
		}
	}
</script>

<div class="flex flex-col h-full min-h-0 bg-surface text-ink-primary" data-testid="transaction-detail-card">
	<!-- Header -->
	<div class="px-4 py-3 border-b border-border bg-subtle flex items-center justify-between gap-2 shrink-0">
		{#if variant === 'panel'}
			<button
				type="button"
				data-testid="tx-detail-close"
				onclick={onClose}
				class="px-2 py-1 -ml-2 rounded-lg text-xs font-semibold text-accent-pop hover:bg-surface-hover transition-colors"
			>
				← All Transactions
			</button>
			<span class="text-xs font-bold uppercase tracking-wider text-ink-secondary">
				Transaction Details
			</span>
		{:else}
			<h2 class="text-sm font-bold text-ink-primary">Transaction Details</h2>
			<button
				type="button"
				data-testid="tx-detail-close"
				aria-label="Close transaction details"
				onclick={onClose}
				class="p-1.5 rounded-lg text-ink-secondary hover:text-ink-primary hover:bg-surface-hover transition-colors"
			>
				✕
			</button>
		{/if}
	</div>

	<!-- Body -->
	<div class="flex-1 overflow-y-auto overscroll-contain-scroll p-4 space-y-4">
		<!-- Summary Hero -->
		<div class="rounded-xl border border-border bg-subtle/60 p-3.5 space-y-2">
			<div class="flex items-start justify-between gap-3">
				<div class="min-w-0">
					<h3 class="text-base font-bold text-ink-primary break-words">{displayMerchant}</h3>
					{#if showOriginalMerchant}
						<p class="text-[11px] text-ink-muted mt-0.5 break-words">
							Original: {transaction.merchantRaw}
						</p>
					{/if}
				</div>
				<span
					class="text-lg font-mono font-bold shrink-0 {transaction.amount > 0
						? 'text-status-good'
						: 'text-ink-primary'}"
				>
					{formatCurrency(transaction.amount)}
				</span>
			</div>

			<div class="flex flex-wrap items-center gap-1.5">
				{#if isQueued}
					<span class="inline-flex items-center rounded-full bg-accent-tint text-accent-pop border border-accent-pop/30 px-2 py-0.5 text-[11px] font-medium">
						Queued offline
					</span>
				{:else if transaction.status === 'PENDING'}
					<span class="inline-flex items-center rounded-full bg-status-warn-bg text-status-warn px-2 py-0.5 text-[11px] font-medium">
						Pending
					</span>
				{:else}
					<span class="inline-flex items-center rounded-full bg-status-good-bg text-status-good px-2 py-0.5 text-[11px] font-medium">
						Posted
					</span>
				{/if}

				{#if transaction.isTransfer}
					<span class="inline-flex items-center rounded-full border border-border bg-surface px-2 py-0.5 text-[11px] text-ink-secondary">
						Transfer
					</span>
				{/if}

				{#if isOffBudget}
					<span class="inline-flex items-center rounded-full border border-border bg-surface px-2 py-0.5 text-[11px] text-ink-secondary">
						Tracked Account
					</span>
				{/if}

				{#if transaction.settledDelta != null && transaction.settledDelta !== 0}
					<span class="inline-flex items-center rounded-full bg-status-warn-bg text-status-warn border border-status-warn/30 px-2 py-0.5 text-[11px] font-medium">
						Settled Δ {transaction.settledDelta > 0 ? '+' : '-'}${Math.abs(transaction.settledDelta).toFixed(2)}
					</span>
				{/if}

				{#if transaction.categorizedBy === 'RULE'}
					<span
						data-testid="tx-detail-auto-badge"
						class="inline-flex items-center rounded-full border border-border bg-surface px-2 py-0.5 text-[11px] text-ink-secondary"
					>
						Auto-categorized by rule
					</span>
				{/if}

				{#if transaction.needsReview}
					<span class="inline-flex items-center rounded-full bg-status-warn-bg text-status-warn px-2 py-0.5 text-[11px] font-medium">
						Needs review
					</span>
					{#if onApproveReview || onUpdateTransaction}
						<button
							type="button"
							data-testid="tx-detail-approve-btn"
							disabled={saving}
							onclick={() => void handleApprove()}
							class="inline-flex items-center rounded-full border border-status-good/40 bg-status-good-bg px-2.5 py-0.5 text-[11px] font-semibold text-status-good hover:opacity-90 disabled:opacity-50"
						>
							Approve
						</button>
					{/if}
				{/if}
			</div>
		</div>

		<!-- Metadata -->
		<dl class="grid grid-cols-2 gap-3 text-xs rounded-xl border border-border bg-surface p-3.5">
			<div>
				<dt class="text-[11px] text-ink-muted">Date</dt>
				<dd class="font-medium text-ink-primary mt-0.5">{formatDate(transaction.transactionDate)}</dd>
			</div>
			<div>
				<dt class="text-[11px] text-ink-muted">Source</dt>
				<dd class="font-medium text-ink-primary mt-0.5">{originLabel(transaction.origin)}</dd>
			</div>
			<div class="col-span-2">
				<dt class="text-[11px] text-ink-muted">Account</dt>
				<dd class="font-medium text-ink-primary mt-0.5 flex items-center gap-1.5 flex-wrap">
					<span>{account?.name || transaction.accountName || 'Account'}</span>
					{#if account && !account.isOnBudget}
						<span class="text-[10px] px-1.5 py-0.5 rounded bg-subtle border border-border text-ink-secondary">
							Tracked
						</span>
					{/if}
				</dd>
			</div>
			<div class="col-span-2">
				<div class="flex items-center justify-between gap-2">
					<dt class="text-[11px] text-ink-muted">Category</dt>
					{#if canSplit && onReplaceSplits && !isEditingSplits}
						<button
							type="button"
							data-testid="tx-detail-split-btn"
							onclick={openSplitEditor}
							class="text-[11px] font-semibold text-accent-pop hover:underline"
						>
							{hasSplits ? 'Edit Splits' : 'Split Transaction'}
						</button>
					{/if}
				</div>
				<dd class="font-medium text-ink-primary mt-0.5">
					{#if hasSplits && transaction.splits}
						<ul class="space-y-1 mt-1">
							{#each transaction.splits as split (split.id)}
								{@const splitCat = split.categoryId ? categoryById.get(split.categoryId) : null}
								{@const splitInc = split.plannedIncomeId ? incomeById.get(split.plannedIncomeId) : null}
								<li class="flex items-center justify-between gap-2 text-xs bg-subtle rounded px-2 py-1">
									<div class="min-w-0">
										<span class="block truncate {!splitCat && !splitInc ? 'text-status-warn font-semibold' : ''}">
											{splitInc
												? `Income — ${splitInc.name}`
												: splitCat
													? `${splitCat.groupName} — ${splitCat.name}`
													: 'Uncategorized (Needs review)'}
										</span>
										{#if split.userNotes}
											<span class="block text-[10px] text-ink-muted truncate">{split.userNotes}</span>
										{/if}
									</div>
									<span class="font-mono shrink-0">{formatCurrency(split.amount)}</span>
								</li>
							{/each}
						</ul>
					{:else if transaction.plannedIncomeId}
						{@const inc = incomeById.get(transaction.plannedIncomeId)}
						<span>Income — {inc ? inc.name : 'Assigned Income'}</span>
					{:else if transaction.categoryId}
						{@const cat = categoryById.get(transaction.categoryId)}
						<span>{cat ? `${cat.groupName} — ${cat.name}` : transaction.categoryName ?? 'Categorized'}</span>
					{:else}
						<span class="text-ink-muted">Uncategorized</span>
					{/if}
				</dd>
			</div>
		</dl>

		{#if isEditingSplits && canSplit && onReplaceSplits}
			<form
				onsubmit={handleSaveSplits}
				data-testid="tx-split-editor"
				class="rounded-xl border border-accent-pop/35 bg-subtle/60 p-3.5 space-y-3"
			>
				<div class="flex items-center justify-between gap-2">
					<div>
						<p class="text-xs font-bold text-ink-primary">Split Transaction</p>
						<p class="text-[11px] text-ink-secondary">
							Total: <span class="font-mono font-semibold text-ink-primary">{formatCurrency(transaction.amount)}</span>
							· Remaining:
							<span
								data-testid="split-remaining-amount"
								class="font-mono font-semibold {Math.abs(splitRemaining) < 0.005
									? 'text-status-good'
									: 'text-status-warn'}"
							>
								${splitRemaining.toFixed(2)}
							</span>
						</p>
					</div>
					<button
						type="button"
						onclick={() => (isEditingSplits = false)}
						class="text-xs text-ink-muted hover:text-ink-primary px-1.5 py-0.5 rounded"
					>
						Cancel
					</button>
				</div>

				<div class="space-y-2">
					{#each splitRows as row, idx (idx)}
						<div
							data-testid="split-editor-row"
							class="rounded-lg border border-border bg-surface p-2.5 space-y-2"
						>
							<div class="flex items-center gap-2">
								<div class="flex-1 min-w-0">
									<select
										aria-label={`Split ${idx + 1} category`}
										bind:value={row.target}
										class="w-full rounded-md border border-border bg-canvas px-2 py-1.5 text-xs text-ink-primary focus:border-accent-pop focus:outline-none"
									>
										<option value="">Select category...</option>
										{#if transaction.amount > 0 && incomes.length > 0}
											<optgroup label="Income">
												{#each incomes as inc (inc.id)}
													<option value={`income:${inc.id}`}>{inc.name}</option>
												{/each}
											</optgroup>
										{/if}
										{#each groupedCategories as [grp, cats] (grp)}
											<optgroup label={grp}>
												{#each cats as cat (cat.id)}
													<option value={cat.id}>{cat.name}</option>
												{/each}
											</optgroup>
										{/each}
									</select>
								</div>

								<div class="w-28 shrink-0 flex items-center gap-1">
									<span class="text-xs font-mono text-ink-muted">{parentSign < 0 ? '-$' : '+$'}</span>
									<input
										type="number"
										step="0.01"
										min="0.01"
										aria-label={`Split ${idx + 1} amount`}
										bind:value={row.amount}
										placeholder="0.00"
										class="w-full rounded-md border border-border bg-canvas px-2 py-1.5 text-xs font-mono text-ink-primary focus:border-accent-pop focus:outline-none"
									/>
								</div>

								{#if splitRows.length > 2}
									<button
										type="button"
										aria-label={`Remove split ${idx + 1}`}
										onclick={() => removeSplitRow(idx)}
										class="p-1 text-xs text-ink-muted hover:text-status-danger rounded"
									>
										✕
									</button>
								{/if}
							</div>

							<div class="flex items-center justify-between gap-2">
								<input
									type="text"
									aria-label={`Split ${idx + 1} note`}
									placeholder="Optional note..."
									bind:value={row.notes}
									class="flex-1 rounded-md border border-border bg-canvas px-2 py-1 text-[11px] text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
								/>
								{#if Math.abs(splitRemaining) >= 0.005}
									<button
										type="button"
										data-testid={`split-fill-remaining-${idx}`}
										onclick={() => fillSplitRemaining(idx)}
										class="shrink-0 text-[11px] font-medium text-accent-pop hover:underline"
									>
										Fill remaining
									</button>
								{/if}
							</div>
						</div>
					{/each}
				</div>

				{#if splitError}
					<p role="alert" class="text-xs text-status-danger bg-status-danger-bg border border-status-danger/30 rounded-lg px-2.5 py-1.5">
						{splitError}
					</p>
				{/if}

				<div class="flex flex-wrap items-center justify-between gap-2 pt-1">
					<div class="flex items-center gap-2">
						<button
							type="button"
							data-testid="split-add-row-btn"
							onclick={addSplitRow}
							class="rounded-lg border border-border bg-surface px-2.5 py-1.5 text-xs font-medium text-ink-primary hover:bg-surface-hover"
						>
							+ Add split
						</button>
						{#if hasSplits}
							<button
								type="button"
								data-testid="split-unsplit-btn"
								disabled={savingSplits}
								onclick={() => void handleUnsplit()}
								class="rounded-lg border border-status-warn/40 px-2.5 py-1.5 text-xs font-medium text-status-warn hover:bg-status-warn-bg disabled:opacity-50"
							>
								Unsplit
							</button>
						{/if}
					</div>

					<button
						type="submit"
						data-testid="split-save-btn"
						disabled={savingSplits}
						class="rounded-lg bg-accent-primary text-accent-primary-text px-3.5 py-1.5 text-xs font-semibold hover:opacity-90 disabled:opacity-50"
					>
						{savingSplits ? 'Saving...' : 'Save Splits'}
					</button>
				</div>
			</form>
		{/if}

		{#if errorMsg}
			<p
				role="alert"
				class="text-xs text-status-danger bg-status-danger-bg border border-status-danger/30 rounded-lg px-3 py-2"
			>
				{errorMsg}
			</p>
		{/if}

		<!-- Edit / Delete Controls -->
		{#if !isQueued && (onUpdateTransaction || onDeleteTransaction)}
			{#if onUpdateTransaction}
				<form onsubmit={handleSave} data-testid="tx-detail-edit-form" class="space-y-3 rounded-xl border border-border bg-subtle/40 p-3.5">
					<p class="text-xs font-semibold text-ink-primary">Edit Details</p>

					<div>
						<label for="tx-detail-merchant" class="block text-[11px] font-medium text-ink-secondary mb-1">
							Merchant
						</label>
						<input
							id="tx-detail-merchant"
							type="text"
							required
							bind:value={editMerchant}
							class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-xs text-ink-primary focus:border-accent-pop focus:outline-none"
						/>
					</div>

					{#if canCategorize}
						{@const isExpenseTx = transaction.amount < 0}
						{@const qCat = categoryFilter.trim()}
						{@const showIncomeSection =
							filteredIncomes.length > 0 &&
							(!isExpenseTx || Boolean(transaction.plannedIncomeId) || qCat.length > 0 || showIncomeInPicker)}
						<div class="space-y-1.5">
							<div class="flex items-center justify-between gap-2">
								<label for="tx-detail-category-search" class="block text-[11px] font-medium text-ink-secondary">
									Category
								</label>
								<span class="text-[11px] font-semibold text-ink-primary truncate max-w-[180px]">
									{selectedTargetLabel}
								</span>
							</div>

							<input
								id="tx-detail-category-search"
								type="search"
								aria-label="Search categories"
								placeholder="Search categories..."
								bind:value={categoryFilter}
								class="w-full rounded-lg border border-border bg-canvas px-3 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
							/>

							<div
								data-testid="tx-detail-category-picker"
								class="max-h-52 overflow-y-auto overscroll-contain-scroll rounded-lg border border-border bg-canvas p-2.5 space-y-2"
							>
								{#if filteredSuggestions.length > 0}
									<div class="space-y-1">
										<span class="block text-[10px] font-semibold uppercase tracking-wider text-accent-pop">
											Suggested
										</span>
										<div class="flex flex-wrap gap-1.5">
											{#each filteredSuggestions as sug (sug.categoryId ?? sug.plannedIncomeId ?? sug.name)}
												<button
													type="button"
													data-testid="tx-detail-suggestion-chip"
													disabled={saving}
													onclick={() => void handleApplySuggestion(sug)}
													class="inline-flex items-center gap-1 rounded-full border border-accent-pop/40 bg-accent-tint/70 px-2.5 py-0.5 text-[11px] font-semibold text-ink-primary hover:bg-accent-tint disabled:opacity-50"
												>
													<span>{sug.name}</span>
													{#if sug.reason}
														<span class="text-[10px] font-normal text-ink-secondary">· {sug.reason}</span>
													{/if}
												</button>
											{/each}
										</div>
									</div>
								{/if}

								{#if filteredGroups.length === 0 && !showIncomeSection && filteredSuggestions.length === 0}
									<p class="text-[11px] text-ink-muted py-1 text-center">No matching categories</p>
								{:else}
									{#if showIncomeSection}
										<div class="space-y-1">
											<p class="text-[10px] font-semibold uppercase tracking-wider text-ink-muted">
												Income
											</p>
											<div class="flex flex-wrap gap-1.5">
												{#each filteredIncomes as inc (inc.id)}
													{@const val = `income:${inc.id}`}
													<button
														type="button"
														onclick={() => (editCategoryId = val)}
														class="rounded-full border px-2.5 py-0.5 text-[11px] font-medium transition-colors {editCategoryId === val
															? 'border-status-good bg-status-good-bg text-status-good font-semibold'
															: 'border-border bg-surface text-ink-primary hover:bg-surface-hover'}"
													>
														{inc.name}
													</button>
												{/each}
											</div>
										</div>
									{/if}

									{#each filteredGroups as [grp, cats] (grp)}
										<div class="space-y-1">
											<p class="text-[10px] font-semibold uppercase tracking-wider text-ink-muted">
												{grp}
											</p>
											<div class="flex flex-wrap gap-1.5">
												{#each cats as cat (cat.id)}
													<button
														type="button"
														data-testid="tx-detail-category-option"
														onclick={() => (editCategoryId = cat.id)}
														class="rounded-full border px-2.5 py-0.5 text-[11px] font-medium transition-colors {editCategoryId === cat.id
															? 'border-accent-pop bg-accent-tint text-accent-pop font-semibold'
															: 'border-border bg-surface text-ink-primary hover:bg-surface-hover'}"
													>
														{cat.name}
													</button>
												{/each}
											</div>
										</div>
									{/each}

									{#if isExpenseTx && !qCat && !showIncomeInPicker && incomes.length > 0 && !transaction.plannedIncomeId}
										<div class="pt-0.5">
											<button
												type="button"
												onclick={() => (showIncomeInPicker = true)}
												class="text-[10px] font-medium text-ink-muted hover:text-accent-pop"
											>
												Show income options
											</button>
										</div>
									{/if}
								{/if}

								{#if editCategoryId !== ''}
									<div class="pt-1 border-t border-border">
										<button
											type="button"
											data-testid="tx-detail-category-clear"
											onclick={() => (editCategoryId = '')}
											class="rounded-full border border-border bg-surface px-2.5 py-0.5 text-[11px] font-medium text-ink-secondary hover:text-ink-primary hover:bg-surface-hover"
										>
											Uncategorized
										</button>
									</div>
								{/if}
							</div>
						</div>
					{/if}

					<div>
						<label for="tx-detail-notes" class="block text-[11px] font-medium text-ink-secondary mb-1">
							Notes
						</label>
						<input
							id="tx-detail-notes"
							type="text"
							placeholder="Optional note..."
							bind:value={editNotes}
							class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
						/>
					</div>

					<div class="flex items-center justify-between gap-2 pt-1">
						{#if !isQueued && !transaction.isTransfer && !isOffBudget && onCreateRule}
							<button
								type="button"
								data-testid="tx-detail-create-rule-btn"
								onclick={() => onCreateRule(transaction)}
								class="px-3 py-2 rounded-lg border border-border bg-surface text-xs font-medium text-accent-pop hover:bg-surface-hover transition-colors"
							>
								Create Rule
							</button>
						{:else}
							<span></span>
						{/if}
						<button
							type="submit"
							disabled={saving || deleting || !editMerchant.trim()}
							class="px-4 py-2 rounded-lg bg-accent-primary text-accent-primary-text text-xs font-semibold hover:opacity-90 disabled:opacity-50 transition-opacity"
						>
							{saving ? 'Saving...' : 'Save Changes'}
						</button>
					</div>
				</form>
			{/if}

			{#if onDeleteTransaction}
				<div class="pt-1">
					{#if confirmDelete}
						<div class="rounded-xl border border-status-danger/40 bg-status-danger-bg p-3 space-y-2">
							<p class="text-xs text-status-danger font-medium">
								Delete this transaction? This cannot be undone.
							</p>
							<div class="flex items-center justify-end gap-2">
								<button
									type="button"
									disabled={deleting}
									onclick={() => (confirmDelete = false)}
									class="px-3 py-1.5 rounded-lg border border-border bg-surface text-xs text-ink-secondary hover:text-ink-primary"
								>
									Cancel
								</button>
								<button
									type="button"
									data-testid="tx-detail-confirm-delete"
									disabled={deleting}
									onclick={() => void handleDeleteConfirmed()}
									class="px-3 py-1.5 rounded-lg bg-status-danger text-accent-primary-text text-xs font-semibold hover:opacity-90 disabled:opacity-50"
								>
									{deleting ? 'Deleting...' : 'Confirm Delete'}
								</button>
							</div>
						</div>
					{:else}
						<button
							type="button"
							data-testid="tx-detail-delete-btn"
							onclick={() => (confirmDelete = true)}
							class="w-full px-3 py-2 rounded-lg border border-status-danger/40 text-xs font-medium text-status-danger hover:bg-status-danger-bg transition-colors"
						>
							Delete Transaction
						</button>
					{/if}
				</div>
			{/if}
		{/if}
	</div>
</div>
