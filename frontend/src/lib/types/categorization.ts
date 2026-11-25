export type RuleMatchType = 'CONTAINS' | 'EXACT';
export type RuleDirection = 'ANY' | 'EXPENSE' | 'INFLOW';
export type SuggestionTargetType = 'CATEGORY' | 'PLANNED_INCOME';

export interface CategorizationRule {
	id: string;
	workspaceId: string;
	priority: number;
	matchType: RuleMatchType;
	merchantPattern: string;
	accountId: string | null;
	direction: RuleDirection;
	minAmount: number | null;
	maxAmount: number | null;
	categoryId: string | null;
	plannedIncomeName: string | null;
	renameMerchant: string | null;
	requireReview: boolean;
	isActive: boolean;
	createdAt: string;
	updatedAt: string;
}

export interface CategorizationSuggestion {
	targetType: SuggestionTargetType;
	categoryId?: string | null;
	plannedIncomeId?: string | null;
	name: string;
	groupName?: string;
	score: number;
	reason: string;
}

export interface CreateRulePayload {
	matchType: RuleMatchType;
	merchantPattern: string;
	accountId?: string | null;
	direction: RuleDirection;
	minAmount?: number | null;
	maxAmount?: number | null;
	categoryId?: string | null;
	plannedIncomeName?: string | null;
	renameMerchant?: string | null;
	requireReview?: boolean;
	isActive?: boolean;
	applyNow?: boolean;
}

export type UpdateRulePayload = Omit<CreateRulePayload, 'applyNow'>;

export interface ApplySimilarPayload {
	merchantPattern: string;
	matchType?: RuleMatchType;
	direction?: RuleDirection;
	categoryId?: string | null;
	plannedIncomeId?: string | null;
	onlyUncategorized?: boolean;
}
