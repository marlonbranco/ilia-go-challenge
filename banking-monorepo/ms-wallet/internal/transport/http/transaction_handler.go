package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"ms-wallet/internal/domain"
	"ms-wallet/internal/usecase"

	"pkg/middleware"
	apiResponse "pkg/response"

	"github.com/go-playground/validator/v10"
	"github.com/shopspring/decimal"
)

type TransactionHandler struct {
	useCase  usecase.TransactionService
	validate *validator.Validate
}

func NewTransactionHandler(useCase usecase.TransactionService) *TransactionHandler {
	return &TransactionHandler{
		useCase:  useCase,
		validate: validator.New(),
	}
}

type createTransactionRequest struct {
	Type        string `json:"type"        validate:"required,oneof=CREDIT DEBIT"`
	Amount      string `json:"amount"      validate:"required"`
	Description string `json:"description"`
}

type transactionResponse struct {
	ID            string `json:"id"`
	UserID        string `json:"user_id"`
	Type          string `json:"type"`
	Amount        string `json:"amount"`
	BalanceBefore string `json:"balance_before"`
	BalanceAfter  string `json:"balance_after"`
	Description   string `json:"description"`
	CreatedAt     string `json:"created_at"`
}

type balanceResponse struct {
	Amount string `json:"amount"`
}

func toTransactionResponse(tx domain.Transaction) transactionResponse {
	return transactionResponse{
		ID:            tx.ID,
		UserID:        tx.UserID,
		Type:          string(tx.Type),
		Amount:        tx.Amount.String(),
		BalanceBefore: tx.BalanceBefore.String(),
		BalanceAfter:  tx.BalanceAfter.String(),
		Description:   tx.Description,
		CreatedAt:     tx.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

func (handler *TransactionHandler) RegisterRoutes(
	mux *http.ServeMux,
	jwtMiddleware func(http.Handler) http.Handler,
	idempotencyMiddleware func(http.Handler) http.Handler,
) {
	mux.Handle("POST /transactions", jwtMiddleware(idempotencyMiddleware(http.HandlerFunc(handler.handleCreate))))
	mux.Handle("GET /transactions", jwtMiddleware(http.HandlerFunc(handler.handleList)))
	mux.Handle("GET /wallet/balance", jwtMiddleware(http.HandlerFunc(handler.handleBalance)))
}

func (handler *TransactionHandler) handleCreate(response http.ResponseWriter, request *http.Request) {
	requestID, _ := middleware.GetRequestID(request.Context())

	claims, ok := middleware.GetClaims(request.Context())
	if !ok {
		writeJSON(response, http.StatusUnauthorized, apiResponse.Error("UNAUTHORIZED", "missing JWT claims", requestID))
		return
	}
	userID, ok := claims["sub"].(string)
	if !ok || userID == "" {
		writeJSON(response, http.StatusUnauthorized, apiResponse.Error("UNAUTHORIZED", "invalid JWT claims", requestID))
		return
	}

	idemKey := request.Header.Get("Idempotency-Key")
	if idemKey == "" {
		writeJSON(response, http.StatusBadRequest, apiResponse.Error("MISSING_IDEMPOTENCY_KEY", "Idempotency-Key header is required", requestID))
		return
	}

	var payload createTransactionRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeJSON(response, http.StatusBadRequest, apiResponse.Error("INVALID_BODY", "invalid JSON body", requestID))
		return
	}

	if err := handler.validate.Struct(payload); err != nil {
		writeJSON(response, http.StatusUnprocessableEntity, apiResponse.Error("VALIDATION_ERROR", err.Error(), requestID))
		return
	}

	amount, err := decimal.NewFromString(payload.Amount)
	if err != nil {
		writeJSON(response, http.StatusUnprocessableEntity, apiResponse.Error("INVALID_AMOUNT", "amount must be a valid decimal number", requestID))
		return
	}

	req := usecase.CreateTransactionRequest{
		UserID:         userID,
		Type:           domain.TransactionType(payload.Type),
		Amount:         amount,
		IdempotencyKey: idemKey,
		Description:    payload.Description,
	}

	tx, err := handler.useCase.CreateTransaction(request.Context(), req)
	if err != nil {
		handler.handleUseCaseError(response, err, requestID)
		return
	}

	writeJSON(response, http.StatusCreated, apiResponse.Success(toTransactionResponse(tx), requestID))
}

func (handler *TransactionHandler) handleList(response http.ResponseWriter, request *http.Request) {
	requestID, _ := middleware.GetRequestID(request.Context())

	claims, ok := middleware.GetClaims(request.Context())
	if !ok {
		writeJSON(response, http.StatusUnauthorized, apiResponse.Error("UNAUTHORIZED", "missing JWT claims", requestID))
		return
	}

	userID, ok := claims["sub"].(string)
	if !ok || userID == "" {
		writeJSON(response, http.StatusUnauthorized, apiResponse.Error("UNAUTHORIZED", "invalid JWT claims", requestID))
		return
	}

	filter := domain.ListFilter{Limit: 20}

	if typeParam := request.URL.Query().Get("type"); typeParam != "" {
		txType := domain.TransactionType(typeParam)
		filter.Type = &txType
	}

	if limitParam := request.URL.Query().Get("limit"); limitParam != "" {
		if l, err := strconv.ParseInt(limitParam, 10, 64); err == nil && l > 0 {
			filter.Limit = l
		}
	}

	if pageParam := request.URL.Query().Get("page"); pageParam != "" {
		if page, err := strconv.ParseInt(pageParam, 10, 64); err == nil && page > 1 {
			filter.Offset = (page - 1) * filter.Limit
		}
	} else if offsetParam := request.URL.Query().Get("offset"); offsetParam != "" {
		if o, err := strconv.ParseInt(offsetParam, 10, 64); err == nil && o >= 0 {
			filter.Offset = o
		}
	}

	txs, err := handler.useCase.ListTransactions(request.Context(), userID, filter)
	if err != nil {
		slog.Error("failed to list transactions", "error", err, "user_id", userID)
		writeJSON(response, http.StatusInternalServerError, apiResponse.Error("INTERNAL_ERROR", "failed to list transactions", requestID))
		return
	}

	results := make([]transactionResponse, 0, len(txs))
	for _, tx := range txs {
		results = append(results, toTransactionResponse(tx))
	}

	writeJSON(response, http.StatusOK, apiResponse.Success(results, requestID))
}

func (handler *TransactionHandler) handleBalance(response http.ResponseWriter, request *http.Request) {
	requestID, _ := middleware.GetRequestID(request.Context())

	claims, ok := middleware.GetClaims(request.Context())
	if !ok {
		writeJSON(response, http.StatusUnauthorized, apiResponse.Error("UNAUTHORIZED", "missing JWT claims", requestID))
		return
	}

	userID, ok := claims["sub"].(string)
	if !ok || userID == "" {
		writeJSON(response, http.StatusUnauthorized, apiResponse.Error("UNAUTHORIZED", "invalid JWT claims", requestID))
		return
	}

	balance, err := handler.useCase.GetBalance(request.Context(), userID)
	if err != nil {
		slog.Error("failed to get balance", "error", err, "user_id", userID)
		writeJSON(response, http.StatusInternalServerError, apiResponse.Error("INTERNAL_ERROR", "failed to get balance", requestID))
		return
	}

	writeJSON(response, http.StatusOK, apiResponse.Success(balanceResponse{Amount: balance.String()}, requestID))
}

func (handler *TransactionHandler) handleUseCaseError(response http.ResponseWriter, err error, requestID string) {
	switch {
	case errors.Is(err, domain.ErrInvalidAmount):
		writeJSON(response, http.StatusUnprocessableEntity, apiResponse.Error("INVALID_AMOUNT", err.Error(), requestID))
	case errors.Is(err, domain.ErrInvalidPrecision):
		writeJSON(response, http.StatusUnprocessableEntity, apiResponse.Error("INVALID_PRECISION", err.Error(), requestID))
	case errors.Is(err, domain.ErrInvalidType):
		writeJSON(response, http.StatusUnprocessableEntity, apiResponse.Error("INVALID_TYPE", err.Error(), requestID))
	case errors.Is(err, domain.ErrInsufficientFunds):
		writeJSON(response, http.StatusUnprocessableEntity, apiResponse.Error("INSUFFICIENT_FUNDS", err.Error(), requestID))
	case errors.Is(err, domain.ErrDuplicate):
		writeJSON(response, http.StatusConflict, apiResponse.Error("DUPLICATE", "idempotency key already used", requestID))
	default:
		slog.Error("unhandled use case error", "error", err)
		writeJSON(response, http.StatusInternalServerError, apiResponse.Error("INTERNAL_ERROR", "an unexpected error occurred", requestID))
	}
}
