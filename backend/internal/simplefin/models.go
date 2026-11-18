package simplefin

type AccountDTO struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Currency     string           `json:"currency"`
	Balance      string           `json:"balance"`
	Transactions []TransactionDTO `json:"transactions,omitempty"`
}

type TransactionDTO struct {
	ID          string `json:"id"`
	AccountID   string `json:"account"`
	Posted      int64  `json:"posted"`
	Amount      string `json:"amount"`
	Description string `json:"description"`
	Pending     bool   `json:"pending"`
}

type DataResponse struct {
	Errors       []string         `json:"errors,omitempty"`
	Accounts     []AccountDTO     `json:"accounts"`
	Transactions []TransactionDTO `json:"transactions,omitempty"`
}
