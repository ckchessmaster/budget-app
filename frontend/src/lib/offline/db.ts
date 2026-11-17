export interface OfflineTransaction {
	localId: string;
	accountId: string;
	amount: number;
	merchantRaw: string;
	categoryId: string | null;
	transactionDate: string;
	syncStatus: 'QUEUED' | 'SYNCING' | 'FAILED';
	createdAt: number;
}

const DB_NAME = 'budget_app_offline_v1';
const DB_VERSION = 1;
const STORE_TX_QUEUE = 'transactions_queue';
const STORE_CATEGORIES = 'cached_categories';
const STORE_ACCOUNTS = 'cached_accounts';

export class BudgetLocalDB {
	private dbPromise: Promise<IDBDatabase> | null = null;

	private open(): Promise<IDBDatabase> {
		if (this.dbPromise) return this.dbPromise;
		this.dbPromise = new Promise((resolve, reject) => {
			const req = indexedDB.open(DB_NAME, DB_VERSION);
			req.onupgradeneeded = () => {
				const db = req.result;
				if (!db.objectStoreNames.contains(STORE_TX_QUEUE)) {
					db.createObjectStore(STORE_TX_QUEUE, { keyPath: 'localId' });
				}
				if (!db.objectStoreNames.contains(STORE_CATEGORIES)) {
					db.createObjectStore(STORE_CATEGORIES, { keyPath: 'id' });
				}
				if (!db.objectStoreNames.contains(STORE_ACCOUNTS)) {
					db.createObjectStore(STORE_ACCOUNTS, { keyPath: 'id' });
				}
			};
			req.onsuccess = () => resolve(req.result);
			req.onerror = () => reject(req.error);
		});
		return this.dbPromise;
	}

	async queueTransaction(tx: OfflineTransaction): Promise<void> {
		const db = await this.open();
		return new Promise((resolve, reject) => {
			const idbTx = db.transaction(STORE_TX_QUEUE, 'readwrite');
			idbTx.objectStore(STORE_TX_QUEUE).put(tx);
			idbTx.oncomplete = () => resolve();
			idbTx.onerror = () => reject(idbTx.error);
		});
	}

	async getPendingQueue(): Promise<OfflineTransaction[]> {
		const db = await this.open();
		return new Promise((resolve, reject) => {
			const idbTx = db.transaction(STORE_TX_QUEUE, 'readonly');
			const req = idbTx.objectStore(STORE_TX_QUEUE).getAll();
			req.onsuccess = () => {
				const items = (req.result as OfflineTransaction[]) ?? [];
				items.sort((a, b) => a.createdAt - b.createdAt);
				resolve(items);
			};
			req.onerror = () => reject(req.error);
		});
	}

	async markSynced(localId: string): Promise<void> {
		const db = await this.open();
		return new Promise((resolve, reject) => {
			const idbTx = db.transaction(STORE_TX_QUEUE, 'readwrite');
			idbTx.objectStore(STORE_TX_QUEUE).delete(localId);
			idbTx.oncomplete = () => resolve();
			idbTx.onerror = () => reject(idbTx.error);
		});
	}

	async cacheCategories<T extends { id: string }>(categories: T[]): Promise<void> {
		const db = await this.open();
		return new Promise((resolve, reject) => {
			const idbTx = db.transaction(STORE_CATEGORIES, 'readwrite');
			const store = idbTx.objectStore(STORE_CATEGORIES);
			store.clear();
			for (const cat of categories) {
				store.put(cat);
			}
			idbTx.oncomplete = () => resolve();
			idbTx.onerror = () => reject(idbTx.error);
		});
	}

	async getCachedCategories<T>(): Promise<T[]> {
		const db = await this.open();
		return new Promise((resolve, reject) => {
			const idbTx = db.transaction(STORE_CATEGORIES, 'readonly');
			const req = idbTx.objectStore(STORE_CATEGORIES).getAll();
			req.onsuccess = () => resolve((req.result as T[]) ?? []);
			req.onerror = () => reject(req.error);
		});
	}

	async cacheAccounts<T extends { id: string }>(accounts: T[]): Promise<void> {
		const db = await this.open();
		return new Promise((resolve, reject) => {
			const idbTx = db.transaction(STORE_ACCOUNTS, 'readwrite');
			const store = idbTx.objectStore(STORE_ACCOUNTS);
			store.clear();
			for (const acct of accounts) {
				store.put(acct);
			}
			idbTx.oncomplete = () => resolve();
			idbTx.onerror = () => reject(idbTx.error);
		});
	}

	async getCachedAccounts<T>(): Promise<T[]> {
		const db = await this.open();
		return new Promise((resolve, reject) => {
			const idbTx = db.transaction(STORE_ACCOUNTS, 'readonly');
			const req = idbTx.objectStore(STORE_ACCOUNTS).getAll();
			req.onsuccess = () => resolve((req.result as T[]) ?? []);
			req.onerror = () => reject(req.error);
		});
	}
}

export const localDB = new BudgetLocalDB();
