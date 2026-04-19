package response_test

import (
	"encoding/json"
	apiResponse"pkg/response"
	"testing"
)

func TestSuccessResponse(test *testing.T) {
	requestID := "req-123"
	data := map[string]string{"user": "alice"}
	
	responseWriter := apiResponse.Success(data, requestID)
	
	if !responseWriter.Success {
		test.Errorf("expected success to be true")
	}
	if responseWriter.RequestID != requestID {
		test.Errorf("expected request_id %q, got %q", requestID, responseWriter.RequestID)
	}
	if responseWriter.Error != nil {
		test.Errorf("expected error to be nil, got %v", responseWriter.Error)
	}
	
	b, err := json.Marshal(responseWriter)
	if err != nil {
		test.Fatalf("failed to marshal: %v", err)
	}
	
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		test.Fatalf("failed to unmarshal: %v", err)
	}
	
	if decoded["success"] != true {
		test.Errorf("JSON success field expected true")
	}
}

func TestErrorResponse(test *testing.T) {
	requestID := "req-456"
	code := "ERR_NOT_FOUND"
	msg := "user not found"
	
	responseWriter := apiResponse.Error(code, msg, requestID)
	
	if responseWriter.Success {
		test.Errorf("expected success to be false")
	}
	if responseWriter.RequestID != requestID {
		test.Errorf("expected request_id %q, got %q", requestID, responseWriter.RequestID)
	}
	if responseWriter.Error == nil {
		test.Fatalf("expected error to not be nil")
	}
	if responseWriter.Error.Code != code {
		test.Errorf("expected error code %q, got %q", code, responseWriter.Error.Code)
	}
	if responseWriter.Error.Message != msg {
		test.Errorf("expected error message %q, got %q", msg, responseWriter.Error.Message)
	}
	
	b, err := json.Marshal(responseWriter)
	if err != nil {
		test.Fatalf("failed to marshal: %v", err)
	}
	
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		test.Fatalf("failed to unmarshal: %v", err)
	}
	
	if decoded["success"] != false {
		test.Errorf("JSON success field expected false")
	}
	if decoded["data"] != nil {
		test.Errorf("JSON data field expected null")
	}
}
