package gateway

import (
	"encoding/json"
	"testing"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	env := Envelope{
		ID:     "test-123",
		Type:   TypeRequest,
		Method: MethodAgentRun,
		Data:   json.RawMessage(`{"prompt":"hello"}`),
	}

	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Envelope
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.ID != "test-123" {
		t.Errorf("ID = %q", decoded.ID)
	}
	if decoded.Type != TypeRequest {
		t.Errorf("Type = %q", decoded.Type)
	}
	if decoded.Method != MethodAgentRun {
		t.Errorf("Method = %q", decoded.Method)
	}
}

func TestEnvelopeWithError(t *testing.T) {
	env := Envelope{
		ID:   "err-1",
		Type: TypeResponse,
		Error: &ErrorPayload{
			Code:    404,
			Message: "not found",
		},
	}

	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Envelope
	json.Unmarshal(data, &decoded)

	if decoded.Error == nil {
		t.Fatal("Error should not be nil")
	}
	if decoded.Error.Code != 404 {
		t.Errorf("Code = %d", decoded.Error.Code)
	}
	if decoded.Error.Message != "not found" {
		t.Errorf("Message = %q", decoded.Error.Message)
	}
}

func TestEnvelopeNoError(t *testing.T) {
	env := Envelope{
		ID:   "ok-1",
		Type: TypeResponse,
		Data: json.RawMessage(`{"status":"ok"}`),
	}

	data, _ := json.Marshal(env)
	var decoded Envelope
	json.Unmarshal(data, &decoded)

	if decoded.Error != nil {
		t.Error("Error should be nil")
	}
}

func TestGatewayStatusRoundTrip(t *testing.T) {
	status := GatewayStatus{
		PID:           12345,
		Uptime:        "5m30s",
		ActiveAgents:  3,
		ActiveClients: 2,
	}

	data, _ := json.Marshal(status)
	var decoded GatewayStatus
	json.Unmarshal(data, &decoded)

	if decoded.PID != 12345 {
		t.Errorf("PID = %d", decoded.PID)
	}
	if decoded.ActiveAgents != 3 {
		t.Errorf("ActiveAgents = %d", decoded.ActiveAgents)
	}
}
