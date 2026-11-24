import type { OfflineTransaction } from '$lib/offline/db';
import type { AccountItem, TransactionItemModel } from '$lib/types/transaction';

/**
 * Maps queued offline transactions for the active month into displayable TransactionItemModel items.
 */
export function mapOfflineQueuedToTransactions(
	queuedTransactions: OfflineTransaction[],
	currentMonth: string
): TransactionItemModel[] {
	return queuedTransactions
		.filter((q) => q.transactionDate.startsWith(currentMonth))
		.map((q) => ({
			id: q.localId,
			accountId: q.accountId,
			origin: 'MANUAL_OFFLINE',
			status: 'PENDING',
			amount: q.amount,
			merchantRaw: q.merchantRaw,
			categoryId: q.categoryId,
			transactionDate: q.transactionDate,
			isCleared: false,
			isTransfer: false,
			syncStatus: q.syncStatus
		}));
}

/**
 * Returns true for uncategorized on-budget non-transfer, non-split transactions.
 */
export function isNewTransaction(
	tx: TransactionItemModel,
	accountById: Map<string, AccountItem>
): boolean {
	if (tx.categoryId !== null) return false;
	if (tx.isTransfer) return false;
	if (tx.splits && tx.splits.length > 0) return false;
	const acct = accountById.get(tx.accountId);
	if (acct && !acct.isOnBudget) return false;
	return true;
}

/**
 * Returns true when a transaction is pending bank settlement or queued offline.
 */
export function isPendingTransaction(tx: TransactionItemModel): boolean {
	return tx.status === 'PENDING' || tx.syncStatus === 'QUEUED';
}

/**
 * Combined To Review predicate: uncategorized on-budget items or pending/queued items.
 */
export function isToReviewTransaction(
	tx: TransactionItemModel,
	accountById: Map<string, AccountItem>
): boolean {
	return isNewTransaction(tx, accountById) || isPendingTransaction(tx);
}

/**
 * Determines the primary review status badge for a transaction row.
 */
export function getReviewBadgeKind(
	tx: TransactionItemModel,
	accountById: Map<string, AccountItem>
): 'pending' | 'new' | null {
	if (isPendingTransaction(tx)) return 'pending';
	if (isNewTransaction(tx, accountById)) return 'new';
	return null;
}
