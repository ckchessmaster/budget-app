package workspace

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type SeedCategory struct {
	Name          string
	GroupName     string
	RolloverMode  string
	MonthlyBudget decimal.Decimal
	TargetAmount  *decimal.Decimal
	TargetCadence string
	SortOrder     int
}

var defaultCategories = []SeedCategory{
	// Essentials
	{Name: "Rent/Mortgage", GroupName: "Essentials", RolloverMode: "SWEEP", MonthlyBudget: decimal.Zero, SortOrder: 1, TargetCadence: "NONE"},
	{Name: "Utilities", GroupName: "Essentials", RolloverMode: "SWEEP", MonthlyBudget: decimal.Zero, SortOrder: 2, TargetCadence: "NONE"},
	{Name: "Groceries", GroupName: "Essentials", RolloverMode: "SWEEP", MonthlyBudget: decimal.Zero, SortOrder: 3, TargetCadence: "NONE"},
	{Name: "Transportation", GroupName: "Essentials", RolloverMode: "SWEEP", MonthlyBudget: decimal.Zero, SortOrder: 4, TargetCadence: "NONE"},

	// Lifestyle
	{Name: "Dining Out", GroupName: "Lifestyle", RolloverMode: "SWEEP", MonthlyBudget: decimal.Zero, SortOrder: 5, TargetCadence: "NONE"},
	{Name: "Entertainment", GroupName: "Lifestyle", RolloverMode: "SWEEP", MonthlyBudget: decimal.Zero, SortOrder: 6, TargetCadence: "NONE"},
	{Name: "Personal Care", GroupName: "Lifestyle", RolloverMode: "SWEEP", MonthlyBudget: decimal.Zero, SortOrder: 7, TargetCadence: "NONE"},

	// Sinking Funds
	{Name: "Auto Maintenance", GroupName: "Sinking Funds", RolloverMode: "ROLLOVER_BALANCE", MonthlyBudget: decimal.Zero, SortOrder: 8, TargetCadence: "NONE"},
	{Name: "Medical Buffer", GroupName: "Sinking Funds", RolloverMode: "ROLLOVER_BALANCE", MonthlyBudget: decimal.Zero, SortOrder: 9, TargetCadence: "NONE"},
	{
		Name:          "Emergency Fund",
		GroupName:     "Sinking Funds",
		RolloverMode:  "TARGET_CAP",
		MonthlyBudget: decimal.Zero,
		TargetAmount:  decimalPtr(decimal.NewFromInt(1000)),
		SortOrder:     10,
		TargetCadence: "NONE",
	},
}

func decimalPtr(d decimal.Decimal) *decimal.Decimal {
	return &d
}

// SeedDefaultCategories inserts the default 10 categories for a newly created workspace
func SeedDefaultCategories(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID) error {
	for _, cat := range defaultCategories {
		var targetAmount *string
		if cat.TargetAmount != nil {
			s := cat.TargetAmount.StringFixed(2)
			targetAmount = &s
		}

		_, err := tx.Exec(ctx, `
			INSERT INTO categories (
				id, workspace_id, name, group_name, monthly_budget,
				rollover_mode, target_amount, target_cadence, sort_order
			) VALUES (
				gen_random_uuid(), $1, $2, $3, $4,
				$5, $6, $7, $8
			)
		`, workspaceID, cat.Name, cat.GroupName, cat.MonthlyBudget,
			cat.RolloverMode, targetAmount, cat.TargetCadence, cat.SortOrder)
		if err != nil {
			return fmt.Errorf("failed to seed category %s: %w", cat.Name, err)
		}
	}
	return nil
}
