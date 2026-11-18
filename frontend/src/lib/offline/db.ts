export type OfflineSyncStatus = 'QUEUED' | 'SYNCING' | 'FAILED';

export interface OfflineTransaction {
	localId: string;
	accountId: string;
	amount: number;
	merchantRaw: string;
	categoryId: string | null;
	transactionDate: string;
	syncStatus: OfflineSyncStatus;
	createdAt: number;
	userNotes?: string;
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
}

const DB_NAME = 'budget_app_offline_v1';
const DB_VERSION = 1;
const STORE_TX_QUEUE = 'transactions_queue';
const STORE_CACHED_CATEGORIES = 'cached_categories';
const STORE_CACHED_ACCOUNTS = 'cached_accounts';

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
				accountId: tx.accountId,
				amount: tx.amount,
				merchantRaw: tx.merchantRaw,
				categoryId: tx.categoryId,
				transactionDate: tx.transactionDate,
				syncStatus: tx.syncStatus,
				createdAt: tx.createdAt,
				userNotes: tx.userNotes
			})
		);
		await done;
	}

	async getPendingQueue(): Promise<OfflineTransaction[]> {
		const db = await this.open();
		const idbTx = db.transaction(STORE_TX_QUEUE, 'readonly');
		const done = txDone(idbTx);
		const store = idbTx.objectStore(STORE_TX_QUEUE);
		const all = await reqToPromise(store.getAll() as IDBRequest<OfflineTransaction[]>);
		await done;
		return all
			.filter((item) => item.syncStatus === 'QUEUED' || item.syncStatus === 'FAILED')
			.sort((a, b) => a.createdAt - b.createdAt);
	}

	async updateSyncStatus(localId: string, syncStatus: OfflineSyncStatus): Promise<void> {
		const db = await this.open();
		const idbTx = db.transaction(STORE_TX_QUEUE, 'readwrite');
		const done = txDone(idbTx);
		const store = idbTx.objectStore(STORE_TX_QUEUE);
		const existing = await reqToPromise(store.get(localId) as IDBRequest<OfflineTransaction | undefined>);
		if (existing) {
			existing.syncStatus = syncStatus;
			await reqToPromise(store.put(existing));
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
				remaining: cat.remaining
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
}
