package budget

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type CreateCategoryRequest struct {
	Name                  string           `json:"name"`
	GroupName             string           `json:"group_name"`
	MonthlyBudget         decimal.Decimal  `json:"monthly_budget"`
	RolloverMode          string           `json:"rollover_mode"`
	SweepTargetCategoryID *uuid.UUID       `json:"sweep_target_category_id"`
	TargetAmount          *decimal.Decimal `json:"target_amount"`
	TargetDate            *string          `json:"target_date"`
	TargetCadence         string           `json:"target_cadence"`
	SortOrder             int              `json:"sort_order"`
}

func validateCategoryFields(name, groupName string, monthlyBudget decimal.Decimal, rolloverMode, targetCadence string) error {
	name = strings.TrimSpace(name)
	if len(name) < 1 || len(name) > 100 {
		return fmt.Errorf("name must be between 1 and 100 characters")
	}
	groupName = strings.TrimSpace(groupName)
	if len(groupName) < 1 || len(groupName) > 100 {
		return fmt.Errorf("group_name must be between 1 and 100 characters")
	}
	if monthlyBudget.LessThan(decimal.Zero) {
		return fmt.Errorf("monthly_budget must be >= 0")
	}
	switch RolloverMode(rolloverMode) {
	case RolloverSweep, RolloverBalance, RolloverTarget:
	default:
		return fmt.Errorf("rollover_mode must be one of SWEEP, ROLLOVER_BALANCE, TARGET_CAP")
	}
	switch targetCadence {
	case "", "NONE", "MONTHLY", "ANNUAL", "TARGET_DATE":
	default:
		return fmt.Errorf("target_cadence must be one of NONE, MONTHLY, ANNUAL, TARGET_DATE")
	}
	return nil
}

func (r *CreateCategoryRequest) Validate() error {
	if r.TargetCadence == "" {
		r.TargetCadence = "NONE"
	}
	return validateCategoryFields(r.Name, r.GroupName, r.MonthlyBudget, r.RolloverMode, r.TargetCadence)
}

type UpdateCategoryRequest struct {
	Name                  string           `json:"name"`
	GroupName             string           `json:"group_name"`
	MonthlyBudget         decimal.Decimal  `json:"monthly_budget"`
	RolloverMode          string           `json:"rollover_mode"`
	SweepTargetCategoryID *uuid.UUID       `json:"sweep_target_category_id"`
	TargetAmount          *decimal.Decimal `json:"target_amount"`
	TargetDate            *string          `json:"target_date"`
	TargetCadence         string           `json:"target_cadence"`
	SortOrder             int              `json:"sort_order"`
}

func (r *UpdateCategoryRequest) Validate() error {
	if r.TargetCadence == "" {
		r.TargetCadence = "NONE"
	}
	return validateCategoryFields(r.Name, r.GroupName, r.MonthlyBudget, r.RolloverMode, r.TargetCadence)
}

type ReallocateFundsDTO struct {
	YearMonth      string          `json:"year_month"`
	FromCategoryID uuid.UUID       `json:"from_category_id"`
	ToCategoryID   uuid.UUID       `json:"to_category_id"`
	Amount         decimal.Decimal `json:"amount"`
	Reason         string          `json:"reason"`
}

func (r *ReallocateFundsDTO) Validate() error {
	if strings.TrimSpace(r.YearMonth) == "" {
		return fmt.Errorf("year_month is required")
	}
	if r.FromCategoryID == uuid.Nil {
		return fmt.Errorf("from_category_id is required")
	}
	if r.ToCategoryID == uuid.Nil {
		return fmt.Errorf("to_category_id is required")
	}
	if !r.Amount.GreaterThan(decimal.Zero) {
		return ErrInvalidAmount
	}
	return nil
}
