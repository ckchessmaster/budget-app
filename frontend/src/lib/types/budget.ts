export type RolloverMode = 'SWEEP' | 'ROLLOVER_BALANCE' | 'TARGET_CAP';
export type TargetCadence = 'NONE' | 'MONTHLY' | 'ANNUAL' | 'TARGET_DATE';

export interface PacingResult {
	monthlyDue: number;
	monthsRemaining: number;
	isMet: boolean;
}

export interface CategoryBudget {
	id: string;
	name: string;
	groupName: string;
	rolloverMode: RolloverMode;
	sweepTargetCategoryId: string | null;
	targetAmount: number | null;
	targetDate: string | null;
	targetCadence: TargetCadence;
	sortOrder: number;
	originalPlanned: number;
	effectiveTarget: number;
	spentCleared: number;
	spentPending: number;
	remaining: number;
	pacing?: PacingResult;
}

export interface MonthSummary {
	yearMonth: string;
	totalIncomeExpected: number;
	totalAllocated: number;
	readyToAssign: number;
	totalSpentCleared: number;
	totalSpentPending: number;
	isUnbalanced: boolean;
}

export interface PlannedIncome {
	id: string;
	workspaceId: string;
	yearMonth: string;
	name: string;
	amount: number;
	createdAt: string;
}

export interface MonthBudgetDetails {
	summary: MonthSummary;
	categories: CategoryBudget[];
	incomes: PlannedIncome[];
}

export interface CreateCategoryPayload {
	name: string;
	group_name: string;
	monthly_budget: number;
	rollover_mode: RolloverMode;
	sweep_target_category_id?: string | null;
	target_amount?: number | null;
	target_date?: string | null;
	target_cadence: TargetCadence;
	sort_order?: number;
}

export interface ReallocatePayload {
	year_month: string;
	from_category_id: string;
	to_category_id: string;
	amount: number;
	reason?: string;
}

export interface CreateIncomePayload {
	year_month: string;
	name: string;
	amount: number;
}

export interface InlineActivityItem {
	id: string;
	transactionId?: string;
	plannedIncomeId?: string | null;
	date: string; // YYYY-MM-DD
	merchant: string;
	accountName: string;
	amount: number;
	status?: 'PENDING' | 'POSTED';
}

export interface SpendRecommendation {
	categoryId: string;
	categoryName: string;
	currentPlanned: number;
	trailingMedianSpend: number;
	percentageDeviation: number;
	shouldPrompt: boolean;
}
