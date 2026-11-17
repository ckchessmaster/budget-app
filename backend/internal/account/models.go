package account

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	ErrNotFound            = errors.New("account not found")
	ErrInvalidCardMode     = errors.New("only credit_card accounts can use DEBT_PAYDOWN mode")
	ErrMissingDebtCategory = errors.New("debt_payoff_category_id is required when card_mode is DEBT_PAYDOWN")
	ErrInvalidAccountType  = errors.New("invalid account type")
	ErrInvalidAmount       = errors.New("amount must be greater than zero")
)

type AccountType string

const (
	Checking   AccountType = "checking"
	Savings    AccountType = "savings"
	CreditCard AccountType = "credit_card"
	Investment AccountType = "investment"
	Loan       AccountType = "loan"
)

type CCMode string

const (
	PayInFull   CCMode = "PAY_IN_FULL"
	DebtPaydown CCMode = "DEBT_PAYDOWN"
)

type Account struct {
	ID                   uuid.UUID       `json:"id"`
	WorkspaceID          uuid.UUID       `json:"workspace_id"`
	ExternalAccountID    *string         `json:"external_account_id"`
	Name                 string          `json:"name"`
	Type                 AccountType     `json:"type"`
	CurrentBalance       decimal.Decimal `json:"current_balance"`
	IsOnBudget           bool            `json:"is_on_budget"`
	IsCreditCard         bool            `json:"is_credit_card"`
	CardMode             CCMode          `json:"card_mode"`
	InitialDebtBaseline  decimal.Decimal `json:"initial_debt_baseline"`
	APRPercentage        decimal.Decimal `json:"apr_percentage"`
	StatementDueDay      *int            `json:"statement_due_day"`
	DebtPayoffCategoryID *uuid.UUID      `json:"debt_payoff_category_id"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

type SolvencyReport struct {
	TotalLiquidCash     decimal.Decimal `json:"total_liquid_cash"`
	TotalCreditCardDebt decimal.Decimal `json:"total_credit_card_debt"`
	SolvencyBuffer      decimal.Decimal `json:"solvency_buffer"`
	IsFloatingDebt      bool            `json:"is_floating_debt"`
	FloatingDebtAmount  decimal.Decimal `json:"floating_debt_amount"`
}

type DebtProjectionInput struct {
	CurrentBalance    decimal.Decimal `json:"current_balance"`
	MonthlyAllocation decimal.Decimal `json:"monthly_allocation"`
	APRPercentage     decimal.Decimal `json:"apr_percentage"`
}

type DebtProjectionResult struct {
	MonthsToPayoff      int             `json:"months_to_payoff"`
	TotalInterestPaid   decimal.Decimal `json:"total_interest_paid"`
	ProjectedPayoffDate time.Time       `json:"projected_payoff_date"`
	IsUnpayable         bool            `json:"is_unpayable"`
}

type PaymentSplitResult struct {
	TransferLegAmount decimal.Decimal `json:"transfer_leg_amount"`
	DebtLegAmount     decimal.Decimal `json:"debt_leg_amount"`
	DebtCategoryID    uuid.UUID       `json:"debt_category_id"`
}
