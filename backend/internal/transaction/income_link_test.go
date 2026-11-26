package transaction

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type mockIncomeLinkRepo struct {
	tx *Transaction
}

func (m *mockIncomeLinkRepo) Create(_ context.Context, _ pgx.Tx, t *Transaction) error {
	m.tx = t
	return nil
}

func (m *mockIncomeLinkRepo) GetByID(_ context.Context, _ pgx.Tx, id uuid.UUID) (*Transaction, error) {
	if m.tx == nil || m.tx.ID != id {
		return nil, ErrTransactionNotFound
	}
	cp := *m.tx
	return &cp, nil
}

func (m *mockIncomeLinkRepo) GetByClientID(_ context.Context, _ pgx.Tx, _ string) (*Transaction, error) {
	return nil, ErrTransactionNotFound
}

func (m *mockIncomeLinkRepo) List(_ context.Context, _ pgx.Tx, _ TransactionFilter) ([]Transaction, int, error) {
	return nil, 0, nil
}

func (m *mockIncomeLinkRepo) Update(_ context.Context, _ pgx.Tx, t *Transaction) error {
	cp := *t
	m.tx = &cp
	return nil
}

func (m *mockIncomeLinkRepo) Delete(_ context.Context, _ pgx.Tx, _ uuid.UUID) error {
	return nil
}

func (m *mockIncomeLinkRepo) DeleteSplitsByParent(_ context.Context, _ pgx.Tx, _ uuid.UUID) error {
	return nil
}

func (m *mockIncomeLinkRepo) PropagateParentFieldsToSplits(_ context.Context, _ pgx.Tx, _ *Transaction) error {
	return nil
}

func (m *mockIncomeLinkRepo) IgnoreReviewBefore(_ context.Context, _ pgx.Tx, _ time.Time) (int64, error) {
	return 0, nil
}

func TestUpdateTransactionPlannedIncomeMutualExclusion(t *testing.T) {
	ctx := context.Background()
	txID := uuid.New()
	catID := uuid.New()
	incomeID := uuid.New()

	repo := &mockIncomeLinkRepo{
		tx: &Transaction{
			ID:         txID,
			AccountID:  uuid.New(),
			Amount:     decimal.NewFromFloat(86.00),
			CategoryID: &catID,
		},
	}
	svc := NewService(repo, nil)

	// Assigning planned income clears any previously assigned category.
	updated, err := svc.UpdateTransaction(ctx, nil, txID, UpdateTransactionRequest{
		PlannedIncomeID: &incomeID,
	})
	if err != nil {
		t.Fatalf("assign planned income failed: %v", err)
	}
	if updated.PlannedIncomeID == nil || *updated.PlannedIncomeID != incomeID {
		t.Fatalf("expected planned_income_id=%s, got %v", incomeID, updated.PlannedIncomeID)
	}
	if updated.CategoryID != nil {
		t.Fatalf("expected category_id to be cleared when assigning planned income, got %v", updated.CategoryID)
	}

	// Assigning category clears planned income.
	updated, err = svc.UpdateTransaction(ctx, nil, txID, UpdateTransactionRequest{
		CategoryID: &catID,
	})
	if err != nil {
		t.Fatalf("assign category failed: %v", err)
	}
	if updated.CategoryID == nil || *updated.CategoryID != catID {
		t.Fatalf("expected category_id=%s, got %v", catID, updated.CategoryID)
	}
	if updated.PlannedIncomeID != nil {
		t.Fatalf("expected planned_income_id to be cleared when assigning category, got %v", updated.PlannedIncomeID)
	}

	// ClearPlannedIncome clears planned income explicitly.
	_, _ = svc.UpdateTransaction(ctx, nil, txID, UpdateTransactionRequest{PlannedIncomeID: &incomeID})
	updated, err = svc.UpdateTransaction(ctx, nil, txID, UpdateTransactionRequest{ClearPlannedIncome: true})
	if err != nil {
		t.Fatalf("clear planned income failed: %v", err)
	}
	if updated.PlannedIncomeID != nil {
		t.Fatalf("expected planned_income_id to be nil after ClearPlannedIncome, got %v", updated.PlannedIncomeID)
	}

	// ApproveReview clears needs_review without overwriting rule provenance.
	repo.tx = &Transaction{
		ID:            txID,
		AccountID:     uuid.New(),
		Amount:        decimal.NewFromFloat(-25.00),
		CategoryID:    &catID,
		CategorizedBy: "RULE",
		NeedsReview:   true,
	}
	approved, err := svc.UpdateTransaction(ctx, nil, txID, UpdateTransactionRequest{ApproveReview: true})
	if err != nil {
		t.Fatalf("approve review failed: %v", err)
	}
	if approved.NeedsReview || approved.CategorizedBy != "RULE" {
		t.Fatalf("expected NeedsReview=false and CategorizedBy=RULE, got needsReview=%v by=%s", approved.NeedsReview, approved.CategorizedBy)
	}

	// Changing category resets rule provenance to MANUAL.
	catID2 := uuid.New()
	manual, err := svc.UpdateTransaction(ctx, nil, txID, UpdateTransactionRequest{CategoryID: &catID2})
	if err != nil {
		t.Fatalf("manual recategorize failed: %v", err)
	}
	if manual.CategorizedBy != "MANUAL" || manual.NeedsReview {
		t.Fatalf("expected CategorizedBy=MANUAL and NeedsReview=false, got by=%s needsReview=%v", manual.CategorizedBy, manual.NeedsReview)
	}
}
