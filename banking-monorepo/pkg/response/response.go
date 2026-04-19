package response

type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Envelope struct {
	Success   bool         `json:"success"`
	Data      any          `json:"data"`
	Error     *ErrorDetail `json:"error"`
	RequestID string       `json:"request_id"`
}

func Success(data any, requestID string) Envelope {
	return Envelope{
		Success:   true,
		Data:      data,
		Error:     nil,
		RequestID: requestID,
	}
}

func Error(code, message, requestID string) Envelope {
	return Envelope{
		Success:   false,
		Data:      nil,
		Error:     &ErrorDetail{Code: code, Message: message},
		RequestID: requestID,
	}
}
