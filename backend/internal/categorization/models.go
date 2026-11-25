package categorization

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const (
	MatchTypeContains = "CONTAINS"
	MatchTypeExact    = "EXACT"

	DirectionAny     = "ANY"
	DirectionExpense = "EXPENSE"
	DirectionInflow  = "INFLOW"

	TargetTypeCategory      = "CATEGORY"
	TargetTypePlannedIncome = "PLANNED_INCOME"
)

var (
	ErrRuleNotFound        = errors.New("categorization rule not found")
	ErrInvalidRule         = errors.New("invalid categorization rule")
	ErrInvalidApplyRequest = errors.New("invalid apply request")
)

type Rule struct {
	ID                uuid.UUID        `json:"id"`
	WorkspaceID       uuid.UUID        `json:"workspace_id"`
	Priority          int              `json:"priority"`
	MatchType         string           `json:"match_type"`
	MerchantPattern   string           `json:"merchant_pattern"`
	AccountID         *uuid.UUID       `json:"account_id"`
	Direction         string           `json:"direction"`
	MinAmount         *decimal.Decimal `json:"min_amount"`
	MaxAmount         *decimal.Decimal `json:"max_amount"`
	CategoryID        *uuid.UUID       `json:"category_id"`
	PlannedIncomeName *string          `json:"planned_income_name"`
	RenameMerchant    *string          `json:"rename_merchant"`
	RequireReview     bool             `json:"require_review"`
	IsActive          bool             `json:"is_active"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
}

type Suggestion struct {
	TargetType      string     `json:"target_type"`
	CategoryID      *uuid.UUID `json:"category_id,omitempty"`
	PlannedIncomeID *uuid.UUID `json:"planned_income_id,omitempty"`
	Name            string     `json:"name"`
	GroupName       string     `json:"group_name,omitempty"`
	Score           float64    `json:"score"`
	Reason          string     `json:"reason"`
}

type CreateRuleRequest struct {
	MatchType         string           `json:"match_type"`
	MerchantPattern   string           `json:"merchant_pattern"`
	AccountID         *uuid.UUID       `json:"account_id"`
	Direction         string           `json:"direction"`
	MinAmount         *decimal.Decimal `json:"min_amount"`
	MaxAmount         *decimal.Decimal `json:"max_amount"`
	CategoryID        *uuid.UUID       `json:"category_id"`
	PlannedIncomeName *string          `json:"planned_income_name"`
	RenameMerchant    *string          `json:"rename_merchant"`
	RequireReview     bool             `json:"require_review"`
	IsActive          *bool            `json:"is_active"`
	ApplyNow          bool             `json:"apply_now"`
}

type UpdateRuleRequest struct {
	MatchType         string           `json:"match_type"`
	MerchantPattern   string           `json:"merchant_pattern"`
	AccountID         *uuid.UUID       `json:"account_id"`
	Direction         string           `json:"direction"`
	MinAmount         *decimal.Decimal `json:"min_amount"`
	MaxAmount         *decimal.Decimal `json:"max_amount"`
	CategoryID        *uuid.UUID       `json:"category_id"`
	PlannedIncomeName *string          `json:"planned_income_name"`
	RenameMerchant    *string          `json:"rename_merchant"`
	RequireReview     bool             `json:"require_review"`
	IsActive          *bool            `json:"is_active"`
}

type ReorderRulesRequest struct {
	RuleIDs []uuid.UUID `json:"rule_ids"`
}

type ApplySimilarRequest struct {
	MerchantPattern   string     `json:"merchant_pattern"`
	MatchType         string     `json:"match_type"`
	Direction         string     `json:"direction,omitempty"`
	CategoryID        *uuid.UUID `json:"category_id"`
	PlannedIncomeID   *uuid.UUID `json:"planned_income_id"`
	OnlyUncategorized *bool      `json:"only_uncategorized"`
}

type ApplyResult struct {
	UpdatedCount int `json:"updated_count"`
}

type CreateRuleResponse struct {
	Rule         Rule `json:"rule"`
	AppliedCount int  `json:"applied_count"`
}
