export type AccountType = 'checking' | 'savings' | 'credit_card' | 'investment' | 'loan';
export type CCMode = 'PAY_IN_FULL' | 'DEBT_PAYDOWN';
export type TransactionOrigin = 'BANK_FEED' | 'MANUAL_OFFLINE' | 'RECURRING';
export type TransactionStatus = 'PENDING' | 'POSTED';

export interface Account {
	id: string;
	workspaceId: string;
	name: string;
	type: AccountType;
	currentBalance: number;
	isOnBudget: boolean;
	isCreditCard: boolean;
	cardMode: CCMode;
	aprPercentage: number;
	debtPayoffCategoryId: string | null;
}

export interface SolvencyReport {
	totalLiquidCash: number;
	totalCreditCardDebt: number;
	solvencyBuffer: number;
	isFloatingDebt: boolean;
	floatingDebtAmount: number;
}

export interface TransactionView {
	id: string;
	accountId: string;
	accountName?: string;
	origin: TransactionOrigin;
	status: TransactionStatus;
	amount: number;
	merchantRaw: string;
	merchantClean: string | null;
	categoryId: string | null;
	categoryName?: string;
	transactionDate: string;
	userNotes: string | null;
	isCleared: boolean;
	syncStatus?: 'QUEUED' | 'SYNCING' | 'FAILED';
	settledDelta?: number;
}

export interface CreateTransactionPayload {
	account_id: string;
	amount: number;
	merchant_raw: string;
	category_id: string | null;
	transaction_date: string;
	user_notes?: string | null;
	origin?: TransactionOrigin;
	status?: TransactionStatus;
	is_cleared?: boolean;
}
