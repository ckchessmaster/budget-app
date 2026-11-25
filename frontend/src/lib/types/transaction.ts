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
	plannedIncomeId?: string | null;
	transactionDate: string;
	userNotes?: string | null;
	isCleared: boolean;
	isTransfer?: boolean;
	syncStatus?: 'QUEUED' | 'SYNCING' | 'FAILED';
	settledDelta?: number | null;
	splits?: TransactionSplit[];
}

export interface UpdateTransactionPayload {
	categoryId?: string | null;
	clearCategory?: boolean;
	plannedIncomeId?: string | null;
	clearPlannedIncome?: boolean;
	merchantClean?: string;
	userNotes?: string;
	status?: TransactionStatus;
	isCleared?: boolean;
	amount?: number;
}

export type AccountType = 'checking' | 'savings' | 'cash' | 'credit_card' | 'investment' | 'loan';

export interface AccountItem {
	id: string;
	name: string;
	type: AccountType;
	currentBalance: number;
	isOnBudget: boolean;
	cardMode: 'PAY_IN_FULL' | 'DEBT_PAYDOWN';
	aprPercentage: number;
	externalAccountId: string | null;
	initialDebtBaseline: number;
	statementDueDay: number | null;
	debtPayoffCategoryId: string | null;
}

export interface CreateAccountPayload {
	name: string;
	type: AccountType;
	currentBalance: number;
	isOnBudget: boolean;
	cardMode?: 'PAY_IN_FULL' | 'DEBT_PAYDOWN';
	initialDebtBaseline?: number;
	aprPercentage?: number;
	statementDueDay?: number | null;
	debtPayoffCategoryId?: string | null;
}

export interface UpdateAccountPayload {
	name?: string;
	type?: AccountType;
	currentBalance?: number;
	isOnBudget?: boolean;
	cardMode?: 'PAY_IN_FULL' | 'DEBT_PAYDOWN';
	initialDebtBaseline?: number;
	aprPercentage?: number;
	statementDueDay?: number | null;
	clearStatementDueDay?: boolean;
	debtPayoffCategoryId?: string | null;
	clearDebtPayoffCategory?: boolean;
	unlinkExternal?: boolean;
	recordBalanceAdjustment?: boolean;
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
