export type TransactionOrigin = 'BANK_FEED' | 'MANUAL_OFFLINE' | 'RECURRING';
export type TransactionStatus = 'PENDING' | 'POSTED';

export interface TransactionSplit {
	id: string;
	amount: number;
	categoryId: string | null;
	userNotes: string | null;
}

export interface TransactionItemModel {
	id: string;
	accountId: string;
	accountName?: string;
	origin: TransactionOrigin;
	status: TransactionStatus;
	amount: number;
	merchantRaw: string;
	merchantClean?: string | null;
	categoryId: string | null;
	categoryName?: string | null;
	transactionDate: string;
	userNotes?: string | null;
	isCleared: boolean;
	syncStatus?: 'QUEUED' | 'SYNCING' | 'FAILED';
	settledDelta?: number | null;
	splits?: TransactionSplit[];
}

export interface AccountItem {
	id: string;
	name: string;
	type: 'checking' | 'savings' | 'credit_card' | 'investment' | 'loan';
	currentBalance: number;
	isOnBudget: boolean;
	cardMode: 'PAY_IN_FULL' | 'DEBT_PAYDOWN';
	aprPercentage: number;
}

export interface SolvencyReport {
	totalLiquidCash: number;
	totalCreditCardDebt: number;
	solvencyBuffer: number;
	isFloatingDebt: boolean;
	floatingDebtAmount: number;
}

export interface CreateTransactionPayload {
	accountId: string;
	totalAmount: number;
	merchantRaw: string;
	categoryId?: string | null;
	transactionDate: string;
	userNotes?: string;
	splits?: Array<{
		amount: number;
		categoryId: string | null;
		userNotes?: string;
	}>;
}
