export type OfflineSyncStatus = 'QUEUED' | 'SYNCING' | 'FAILED';

export interface OfflineTransaction {
	localId: string;
	workspaceId?: string;
	accountId: string;
	amount: number;
	merchantRaw: string;
	categoryId: string | null;
	transactionDate: string;
	syncStatus: OfflineSyncStatus;
	createdAt: number;
	userNotes?: string;
	lastError?: string;
}

export interface CachedAccount {
	id: string;
	name: string;
	type: string;
	currentBalance: number;
	isOnBudget: boolean;
	cardMode?: string;
}

export interface CachedCategory {
	id: string;
	name: string;
	groupName: string;
	remaining: number;
	originalPlanned?: number;
	effectiveTarget?: number;
	spentCleared?: number;
	spentPending?: number;
	sortOrder?: number;
}

export interface CachedBudgetSnapshot {
	yearMonth: string;
	summary: {
		yearMonth: string;
		totalIncomeExpected: number;
		totalAllocated: number;
		readyToAssign: number;
		totalSpentCleared: number;
		totalSpentPending: number;
		isUnbalanced: boolean;
	};
	incomes: Array<{
		id: string;
		workspaceId: string;
		yearMonth: string;
		name: string;
		amount: number;
		createdAt: string;
	}>;
	updatedAt: number;
}

const DB_NAME = 'budget_app_offline_v1';
const DB_VERSION = 2;
const STORE_TX_QUEUE = 'transactions_queue';
const STORE_CACHED_CATEGORIES = 'cached_categories';
const STORE_CACHED_ACCOUNTS = 'cached_accounts';
const STORE_CACHED_SNAPSHOTS = 'cached_snapshots';

function reqToPromise<T>(req: IDBRequest<T>): Promise<T> {
	return new Promise((resolve, reject) => {
		req.onsuccess = () => resolve(req.result);
		req.onerror = () => reject(req.error ?? new Error('IndexedDB request failed'));
	});
}

function txDone(tx: IDBTransaction): Promise<void> {
	return new Promise((resolve, reject) => {
		tx.oncomplete = () => resolve();
		tx.onerror = () => reject(tx.error ?? new Error('IndexedDB transaction failed'));
		tx.onabort = () => reject(tx.error ?? new Error('IndexedDB transaction aborted'));
	});
}

export class BudgetLocalDB {
	private idbFactory: IDBFactory | undefined;
	private dbPromise: Promise<IDBDatabase> | null = null;

	constructor(idbFactory?: IDBFactory) {
		this.idbFactory = idbFactory ?? (typeof globalThis !== 'undefined' ? globalThis.indexedDB : undefined);
	}

	private open(): Promise<IDBDatabase> {
		if (this.dbPromise) return this.dbPromise;
		if (!this.idbFactory) {
			return Promise.reject(new Error('IndexedDB is not available in this environment'));
		}

		this.dbPromise = new Promise((resolve, reject) => {
			const request = this.idbFactory!.open(DB_NAME, DB_VERSION);

			request.onupgradeneeded = () => {
				const db = request.result;
				if (!db.objectStoreNames.contains(STORE_TX_QUEUE)) {
					db.createObjectStore(STORE_TX_QUEUE, { keyPath: 'localId' });
				}
				if (!db.objectStoreNames.contains(STORE_CACHED_CATEGORIES)) {
					db.createObjectStore(STORE_CACHED_CATEGORIES, { keyPath: 'id' });
				}
				if (!db.objectStoreNames.contains(STORE_CACHED_ACCOUNTS)) {
					db.createObjectStore(STORE_CACHED_ACCOUNTS, { keyPath: 'id' });
				}
				if (!db.objectStoreNames.contains(STORE_CACHED_SNAPSHOTS)) {
					db.createObjectStore(STORE_CACHED_SNAPSHOTS, { keyPath: 'yearMonth' });
				}
			};

			request.onsuccess = () => resolve(request.result);
			request.onerror = () => reject(request.error ?? new Error('Failed to open IndexedDB'));
		});

		return this.dbPromise;
	}

	async queueTransaction(tx: OfflineTransaction): Promise<void> {
		const db = await this.open();
		const idbTx = db.transaction(STORE_TX_QUEUE, 'readwrite');
		const done = txDone(idbTx);
		const store = idbTx.objectStore(STORE_TX_QUEUE);
		await reqToPromise(
			store.put({
				localId: tx.localId,
				workspaceId: tx.workspaceId,
				accountId: tx.accountId,
				amount: tx.amount,
				merchantRaw: tx.merchantRaw,
				categoryId: tx.categoryId,
				transactionDate: tx.transactionDate,
				syncStatus: tx.syncStatus,
				createdAt: tx.createdAt,
				userNotes: tx.userNotes,
				lastError: tx.lastError
			})
		);
		await done;
	}

	/**
	 * Everything that still owes the server a write.
	 *
	 * SYNCING is included deliberately. The replay is asynchronous now, so an item can be
	 * left in that state by a tab that closed mid-flight, and an item nothing ever picks up
	 * again is a transaction that silently never syncs. Re-sending one is safe: the server
	 * dedupes on the client id, so an item that already landed is confirmed rather than
	 * duplicated.
	 */
	async getPendingQueue(workspaceId?: string): Promise<OfflineTransaction[]> {
		const db = await this.open();
		const idbTx = db.transaction(STORE_TX_QUEUE, 'readonly');
		const done = txDone(idbTx);
		const store = idbTx.objectStore(STORE_TX_QUEUE);
		const all = await reqToPromise(store.getAll() as IDBRequest<OfflineTransaction[]>);
		await done;
		return all
			.filter(
				(item) =>
					(item.syncStatus === 'QUEUED' ||
						item.syncStatus === 'FAILED' ||
						item.syncStatus === 'SYNCING') &&
					(!workspaceId || !item.workspaceId || item.workspaceId === workspaceId)
			)
			.sort((a, b) => a.createdAt - b.createdAt);
	}

	async updateSyncStatus(localId: string, syncStatus: OfflineSyncStatus, lastError?: string): Promise<void> {
		const db = await this.open();
		const idbTx = db.transaction(STORE_TX_QUEUE, 'readwrite');
		const done = txDone(idbTx);
		const store = idbTx.objectStore(STORE_TX_QUEUE);
		const existing = await reqToPromise(store.get(localId) as IDBRequest<OfflineTransaction | undefined>);
		if (existing) {
			existing.syncStatus = syncStatus;
			if (lastError !== undefined) {
				existing.lastError = lastError;
			} else if (syncStatus === 'QUEUED') {
				delete existing.lastError;
			}
			await reqToPromise(store.put(existing));
		}
		await done;
	}

	async updateQueuedTransaction(
		localId: string,
		patch: Partial<
			Pick<
				OfflineTransaction,
				| 'accountId'
				| 'amount'
				| 'merchantRaw'
				| 'categoryId'
				| 'transactionDate'
				| 'userNotes'
				| 'syncStatus'
				| 'lastError'
			>
		>
	): Promise<void> {
		const db = await this.open();
		const idbTx = db.transaction(STORE_TX_QUEUE, 'readwrite');
		const done = txDone(idbTx);
		const store = idbTx.objectStore(STORE_TX_QUEUE);
		const existing = await reqToPromise(store.get(localId) as IDBRequest<OfflineTransaction | undefined>);
		if (existing) {
			const updated: OfflineTransaction = {
				...existing,
				...patch
			};
			if (patch.syncStatus === 'QUEUED' && patch.lastError === undefined) {
				delete updated.lastError;
			}
			await reqToPromise(store.put(updated));
		}
		await done;
	}

	async deleteQueuedTransaction(localId: string): Promise<void> {
		await this.markSynced(localId);
	}

	async clearQueuedForWorkspace(workspaceId: string): Promise<void> {
		if (!workspaceId || !this.idbFactory) return;
		const db = await this.open();
		const idbTx = db.transaction(STORE_TX_QUEUE, 'readwrite');
		const done = txDone(idbTx);
		const store = idbTx.objectStore(STORE_TX_QUEUE);
		const all = await reqToPromise(store.getAll() as IDBRequest<OfflineTransaction[]>);
		for (const item of all) {
			if (item.workspaceId === workspaceId) {
				store.delete(item.localId);
			}
		}
		await done;
	}

	async markSynced(localId: string): Promise<void> {
		const db = await this.open();
		const idbTx = db.transaction(STORE_TX_QUEUE, 'readwrite');
		const done = txDone(idbTx);
		const store = idbTx.objectStore(STORE_TX_QUEUE);
		await reqToPromise(store.delete(localId));
		await done;
	}

	async cacheCategories(categories: CachedCategory[]): Promise<void> {
		const db = await this.open();
		const idbTx = db.transaction(STORE_CACHED_CATEGORIES, 'readwrite');
		const done = txDone(idbTx);
		const store = idbTx.objectStore(STORE_CACHED_CATEGORIES);
		store.clear();
		for (const cat of categories) {
			store.put({
				id: cat.id,
				name: cat.name,
				groupName: cat.groupName,
				remaining: cat.remaining,
				originalPlanned: cat.originalPlanned,
				effectiveTarget: cat.effectiveTarget,
				spentCleared: cat.spentCleared,
				spentPending: cat.spentPending,
				sortOrder: cat.sortOrder
			});
		}
		await done;
	}

	async getCachedCategories(): Promise<CachedCategory[]> {
		const db = await this.open();
		const idbTx = db.transaction(STORE_CACHED_CATEGORIES, 'readonly');
		const done = txDone(idbTx);
		const store = idbTx.objectStore(STORE_CACHED_CATEGORIES);
		const all = await reqToPromise(store.getAll() as IDBRequest<CachedCategory[]>);
		await done;
		return all;
	}

	async cacheAccounts(accounts: CachedAccount[]): Promise<void> {
		const db = await this.open();
		const idbTx = db.transaction(STORE_CACHED_ACCOUNTS, 'readwrite');
		const done = txDone(idbTx);
		const store = idbTx.objectStore(STORE_CACHED_ACCOUNTS);
		store.clear();
		for (const acct of accounts) {
			store.put({
				id: acct.id,
				name: acct.name,
				type: acct.type,
				currentBalance: acct.currentBalance,
				isOnBudget: acct.isOnBudget,
				cardMode: acct.cardMode
			});
		}
		await done;
	}

	async getCachedAccounts(): Promise<CachedAccount[]> {
		const db = await this.open();
		const idbTx = db.transaction(STORE_CACHED_ACCOUNTS, 'readonly');
		const done = txDone(idbTx);
		const store = idbTx.objectStore(STORE_CACHED_ACCOUNTS);
		const all = await reqToPromise(store.getAll() as IDBRequest<CachedAccount[]>);
		await done;
		return all;
	}

	async cacheBudgetSnapshot(snapshot: CachedBudgetSnapshot): Promise<void> {
		const db = await this.open();
		const idbTx = db.transaction(STORE_CACHED_SNAPSHOTS, 'readwrite');
		const done = txDone(idbTx);
		const store = idbTx.objectStore(STORE_CACHED_SNAPSHOTS);
		await reqToPromise(store.put(snapshot));
		await done;
	}

	async getCachedBudgetSnapshot(yearMonth: string): Promise<CachedBudgetSnapshot | undefined> {
		const db = await this.open();
		const idbTx = db.transaction(STORE_CACHED_SNAPSHOTS, 'readonly');
		const done = txDone(idbTx);
		const store = idbTx.objectStore(STORE_CACHED_SNAPSHOTS);
		const result = await reqToPromise(
			store.get(yearMonth) as IDBRequest<CachedBudgetSnapshot | undefined>
		);
		await done;
		return result;
	}

	async clearCaches(): Promise<void> {
		if (!this.idbFactory) return;
		const db = await this.open();
		for (const storeName of [
			STORE_CACHED_CATEGORIES,
			STORE_CACHED_ACCOUNTS,
			STORE_CACHED_SNAPSHOTS
		]) {
			if (!db.objectStoreNames.contains(storeName)) continue;
			const idbTx = db.transaction(storeName, 'readwrite');
			const done = txDone(idbTx);
			idbTx.objectStore(storeName).clear();
			await done;
		}
	}
}
