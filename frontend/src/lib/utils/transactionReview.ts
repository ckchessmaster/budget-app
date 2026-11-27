import type { OfflineTransaction } from '$lib/offline/db';
import type { CategoryBudget } from '$lib/types/budget';
import type { AccountItem, TransactionItemModel } from '$lib/types/transaction';

/**
 * Maps queued offline transactions for the active month into displayable TransactionItemModel items.
 */
export function mapOfflineQueuedToTransactions(
	queuedTransactions: OfflineTransaction[],
	currentMonth: string
): TransactionItemModel[] {
	return queuedTransactions
		.filter((q) => q.transactionDate.startsWith(currentMonth) || q.syncStatus === 'FAILED')
		.map((q) => ({
			id: q.localId,
			accountId: q.accountId,
			origin: 'MANUAL_OFFLINE',
			status: 'PENDING',
			amount: q.amount,
			merchantRaw: q.merchantRaw,
			categoryId: q.categoryId,
			transactionDate: q.transactionDate,
			userNotes: q.userNotes ?? null,
			isCleared: false,
			isTransfer: false,
			syncStatus: q.syncStatus,
			lastError: q.lastError ?? null
		}));
}

export function hasUncategorizedSplit(tx: TransactionItemModel): boolean {
	return Boolean(
		tx.splits &&
			tx.splits.length > 0 &&
			tx.splits.some((s) => s.categoryId === null && !s.plannedIncomeId)
	);
}

/**
 * Returns true for uncategorized on-budget non-transfer transactions (including splits with an uncategorized remainder).
 */
export function isNewTransaction(
	tx: TransactionItemModel,
	accountById: Map<string, AccountItem>
): boolean {
	if (tx.isTransfer) return false;
	const acct = accountById.get(tx.accountId);
	if (acct && !acct.isOnBudget) return false;
	if (tx.splits && tx.splits.length > 0) {
		return hasUncategorizedSplit(tx);
	}
	if (tx.categoryId !== null) return false;
	if (tx.plannedIncomeId) return false;
	return true;
}

/**
 * Returns true when a transaction is pending bank settlement or queued offline.
 */
export function isPendingTransaction(tx: TransactionItemModel): boolean {
	return tx.status === 'PENDING' || tx.origin === 'MANUAL_OFFLINE' && Boolean(tx.syncStatus);
}

export function isOfflineLocalTransaction(tx: TransactionItemModel): boolean {
	return tx.origin === 'MANUAL_OFFLINE' && Boolean(tx.syncStatus);
}

export function effectiveBudgetMonth(tx: TransactionItemModel): string {
	return tx.budgetMonth || tx.transactionDate.slice(0, 7);
}

export function hasBudgetMonthOverride(tx: TransactionItemModel): boolean {
	return Boolean(tx.budgetMonth && tx.budgetMonth !== tx.transactionDate.slice(0, 7));
}

export const MOVE_MONTH_SKIP_DATE_KEY = 'budget_move_month_prompt_skip_date';

export function localCalendarDateKey(now: Date = new Date()): string {
	const y = now.getFullYear();
	const m = String(now.getMonth() + 1).padStart(2, '0');
	const d = String(now.getDate()).padStart(2, '0');
	return `${y}-${m}-${d}`;
}

export function isMoveMonthPromptSkippedToday(now: Date = new Date()): boolean {
	if (typeof window === 'undefined') return false;
	try {
		return window.localStorage.getItem(MOVE_MONTH_SKIP_DATE_KEY) === localCalendarDateKey(now);
	} catch {
		return false;
	}
}

export function skipMoveMonthPromptToday(now: Date = new Date()): void {
	if (typeof window === 'undefined') return;
	try {
		window.localStorage.setItem(MOVE_MONTH_SKIP_DATE_KEY, localCalendarDateKey(now));
	} catch {
		// localStorage unavailable
	}
}

export function buildMoveToMonthPatch(
	tx: TransactionItemModel,
	targetMonth: string
): { budgetMonth?: string; clearBudgetMonth?: boolean } {
	const txMonth = tx.transactionDate.slice(0, 7);
	return targetMonth === txMonth ? { clearBudgetMonth: true } : { budgetMonth: targetMonth };
}

/**
 * Combined To Review predicate: uncategorized on-budget items, pending/queued items,
 * or auto-categorized items flagged for review (unless explicitly ignored from review).
 */
export function isToReviewTransaction(
	tx: TransactionItemModel,
	accountById: Map<string, AccountItem>
): boolean {
	if (tx.reviewIgnored) return false;
	if (isPendingTransaction(tx)) return true;
	if (isNewTransaction(tx, accountById)) return true;
	if (tx.needsReview && !tx.isTransfer) {
		const acct = accountById.get(tx.accountId);
		if (!acct || acct.isOnBudget) return true;
	}
	return false;
}

/**
 * Determines the primary review status badge for a transaction row.
 */
export function getReviewBadgeKind(
	tx: TransactionItemModel,
	accountById: Map<string, AccountItem>
): 'pending' | 'review' | 'new' | null {
	if (tx.reviewIgnored) return null;
	if (isPendingTransaction(tx)) return 'pending';
	if (isNewTransaction(tx, accountById)) return 'new';
	if (tx.needsReview && !tx.isTransfer) {
		const acct = accountById.get(tx.accountId);
		if (!acct || acct.isOnBudget) return 'review';
	}
	return null;
}

/**
 * Returns true if a transaction can be categorized via the budget category picker / Catch Up modal.
 */
export function isCategorizableTransaction(
	tx: TransactionItemModel,
	accountById: Map<string, AccountItem>
): boolean {
	if (tx.syncStatus === 'QUEUED') return false;
	if (tx.isTransfer) return false;
	if (tx.splits && tx.splits.length > 0) return false;
	const acct = accountById.get(tx.accountId);
	if (acct && !acct.isOnBudget) return false;
	return true;
}

/**
 * Returns true for synced, uncategorized, on-budget non-transfer, non-split transactions
 * eligible for the Catch Up prompt & Swipe Stack (both POSTED and PENDING).
 */
export function isCatchUpTransaction(
	tx: TransactionItemModel,
	accountById: Map<string, AccountItem>
): boolean {
	return !tx.reviewIgnored && tx.syncStatus !== 'QUEUED' && isNewTransaction(tx, accountById);
}

const KEYWORD_CATEGORY_HINTS: Array<{ keywords: string[]; categoryTerms: string[] }> = [
	{
		keywords: ['grocery', 'groceries', 'market', 'trader', 'whole foods', 'kroger', 'safeway', 'aldi', 'publix', 'wegmans'],
		categoryTerms: ['grocer', 'supermarket', 'food']
	},
	{
		keywords: ['cafe', 'coffee', 'starbucks', 'restaurant', 'dining', 'chipotle', 'doordash', 'ubereats', 'grubhub', 'pizza', 'bakery', 'bistro'],
		categoryTerms: ['dining', 'restaurant', 'coffee', 'eating out', 'takeout', 'food']
	},
	{
		keywords: ['gas', 'shell', 'chevron', 'exxon', 'mobil', 'bp', 'uber', 'lyft', 'transit', 'metro', 'parking', 'airline'],
		categoryTerms: ['gas', 'fuel', 'transport', 'auto', 'travel', 'commut']
	},
	{
		keywords: ['netflix', 'spotify', 'hulu', 'apple', 'subscription', 'disney', 'youtube', 'patreon', 'icloud'],
		categoryTerms: ['subscription', 'stream', 'entertainment', 'media', 'software']
	},
	{
		keywords: ['electric', 'water', 'utility', 'utilities', 'internet', 'comcast', 'verizon', 'at&t', 't-mobile'],
		categoryTerms: ['utilit', 'internet', 'phone', 'bill']
	},
	{
		keywords: ['pharmacy', 'cvs', 'walgreens', 'clinic', 'dental', 'medical', 'doctor', 'hospital'],
		categoryTerms: ['health', 'medical', 'pharmacy', 'dental']
	}
];

function normalizeMerchant(tx: TransactionItemModel): string {
	return (tx.merchantClean?.trim() || tx.merchantRaw).trim().toLowerCase();
}

function merchantsMatch(a: string, b: string): boolean {
	if (!a || !b) return false;
	if (a === b) return true;
	if (a.length >= 4 && b.length >= 4) {
		return a.includes(b) || b.includes(a);
	}
	return false;
}

/**
 * Suggests up to `maxSuggestions` ordered, deduplicated categories for a transaction
 * based on historical merchant assignments, keyword/name matching, and category list order.
 */
export function suggestCategoriesForTransaction(
	tx: TransactionItemModel,
	categories: CategoryBudget[],
	allTransactions: TransactionItemModel[] = [],
	maxSuggestions = 6
): CategoryBudget[] {
	if (categories.length === 0 || maxSuggestions <= 0) return [];

	const categoryById = new Map(categories.map((c) => [c.id, c]));
	const merchant = normalizeMerchant(tx);
	const result: CategoryBudget[] = [];
	const seenIds = new Set<string>();

	const pushCategory = (cat: CategoryBudget | undefined) => {
		if (!cat || seenIds.has(cat.id) || result.length >= maxSuggestions) return;
		seenIds.add(cat.id);
		result.push(cat);
	};

	// 1. Historical merchant matches sorted by frequency descending
	if (merchant) {
		const counts = new Map<string, number>();
		for (const other of allTransactions) {
			if (other.id === tx.id || !other.categoryId) continue;
			if (!categoryById.has(other.categoryId)) continue;
			const otherMerchant = normalizeMerchant(other);
			if (merchantsMatch(merchant, otherMerchant)) {
				counts.set(other.categoryId, (counts.get(other.categoryId) ?? 0) + 1);
			}
		}
		const rankedIds = Array.from(counts.entries())
			.sort((a, b) => b[1] - a[1])
			.map(([catId]) => catId);
		for (const catId of rankedIds) {
			pushCategory(categoryById.get(catId));
		}
	}

	// 2. Direct token overlap with category name/group & lightweight keyword hints
	if (merchant && result.length < maxSuggestions) {
		const merchantWords = merchant.split(/[^a-z0-9]+/).filter((w) => w.length >= 3);

		for (const cat of categories) {
			if (seenIds.has(cat.id)) continue;
			const catText = `${cat.name} ${cat.groupName}`.toLowerCase();
			if (merchantWords.some((word) => catText.includes(word))) {
				pushCategory(cat);
			}
		}

		for (const hint of KEYWORD_CATEGORY_HINTS) {
			if (!hint.keywords.some((kw) => merchant.includes(kw))) continue;
			for (const cat of categories) {
				if (seenIds.has(cat.id)) continue;
				const catText = `${cat.name} ${cat.groupName}`.toLowerCase();
				if (hint.categoryTerms.some((term) => catText.includes(term))) {
					pushCategory(cat);
				}
			}
		}
	}

	return result;
}
