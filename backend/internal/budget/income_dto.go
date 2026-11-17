package budget

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

type CreatePlannedIncomeRequest struct {
	YearMonth string          `json:"year_month"`
	Name      string          `json:"name"`
	Amount    decimal.Decimal `json:"amount"`
}

func (r *CreatePlannedIncomeRequest) Validate() error {
	if strings.TrimSpace(r.YearMonth) == "" {
		return fmt.Errorf("year_month is required")
	}
	name := strings.TrimSpace(r.Name)
	if len(name) < 1 || len(name) > 150 {
		return fmt.Errorf("name must be between 1 and 150 characters")
	}
	if !r.Amount.GreaterThan(decimal.Zero) {
		return ErrInvalidAmount
	}
	return nil
}
