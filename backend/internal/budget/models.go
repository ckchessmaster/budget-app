package budget

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	ErrNotFound             = errors.New("resource not found")
	ErrCyclicSweep          = errors.New("sweep_target_category_id cannot reference the category itself")
	ErrInvalidAmount        = errors.New("amount must be greater than zero")
	ErrSameCategoryTransfer = errors.New("from_category_id and to_category_id cannot be identical")
)

type RolloverMode string

const (
	RolloverSweep   RolloverMode = "SWEEP"
	RolloverBalance RolloverMode = "ROLLOVER_BALANCE"
	RolloverTarget  RolloverMode = "TARGET_CAP"
)

type Category struct {
	ID                    uuid.UUID        `json:"id"`
	WorkspaceID           uuid.UUID        `json:"workspace_id"`
	Name                  string           `json:"name"`
	GroupName             string           `json:"group_name"`
	MonthlyBudget         decimal.Decimal  `json:"monthly_budget"`
	RolloverMode          RolloverMode     `json:"rollover_mode"`
	SweepTargetCategoryID *uuid.UUID       `json:"sweep_target_category_id"`
	TargetAmount          *decimal.Decimal `json:"target_amount"`
	TargetDate            *time.Time       `json:"target_date"`
	TargetCadence         string           `json:"target_cadence"`
	SortOrder             int              `json:"sort_order"`
}

type MonthlyCategoryBudget struct {
	ID              uuid.UUID       `json:"id"`
	WorkspaceID     uuid.UUID       `json:"workspace_id"`
	CategoryID      uuid.UUID       `json:"category_id"`
	YearMonth       time.Time       `json:"year_month"`
	OriginalPlanned decimal.Decimal `json:"original_planned"`
}

type MonthlyPlannedIncome struct {
	ID          uuid.UUID       `json:"id"`
	WorkspaceID uuid.UUID       `json:"workspace_id"`
	YearMonth   time.Time       `json:"year_month"`
	Name        string          `json:"name"`
	Amount      decimal.Decimal `json:"amount"`
	CreatedAt   time.Time       `json:"created_at"`
}

type ReallocationRequest struct {
	YearMonth      time.Time       `json:"year_month"`
	FromCategoryID uuid.UUID       `json:"from_category_id"`
	ToCategoryID   uuid.UUID       `json:"to_category_id"`
	Amount         decimal.Decimal `json:"amount"`
	Reason         string          `json:"reason"`
}

type MonthSummary struct {
	YearMonth           time.Time       `json:"year_month"`
	TotalIncomeExpected decimal.Decimal `json:"total_income_expected"`
	TotalAllocated      decimal.Decimal `json:"total_allocated"`
	ReadyToAssign       decimal.Decimal `json:"ready_to_assign"`
	TotalSpentCleared   decimal.Decimal `json:"total_spent_cleared"`
	TotalSpentPending   decimal.Decimal `json:"total_spent_pending"`
	IsUnbalanced        bool            `json:"is_unbalanced"`
}

// NormalizeYearMonth normalizes any timestamp to midnight UTC on the 1st of its month.
func NormalizeYearMonth(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
}
