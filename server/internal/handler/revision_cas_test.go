package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpdateAgentInstructionsRevisionCAS(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "revision-cas-agent", nil)
	baseline, err := testHandler.Queries.GetAgent(context.Background(), parseUUID(agentID))
	if err != nil {
		t.Fatalf("load baseline agent: %v", err)
	}

	type result struct {
		code int
		body string
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for _, text := range []string{"writer A", "writer B"} {
		go func(text string) {
			<-start
			w := httptest.NewRecorder()
			testHandler.UpdateAgent(w, withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
				"instructions":      text,
				"expected_revision": baseline.Revision,
			}), "id", agentID))
			results <- result{code: w.Code, body: w.Body.String()}
		}(text)
	}
	close(start)
	var successes, conflicts int
	for range 2 {
		got := <-results
		switch {
		case got.code == http.StatusOK:
			successes++
		case got.code == http.StatusConflict && strings.Contains(got.body, "revision_conflict"):
			conflicts++
		default:
			t.Fatalf("overlapping writer: unexpected response %d: %s", got.code, got.body)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("overlapping writers: successes=%d conflicts=%d, want one each", successes, conflicts)
	}

	afterOverlap, err := testHandler.Queries.GetAgent(context.Background(), parseUUID(agentID))
	if err != nil {
		t.Fatalf("reload after overlapping writers: %v", err)
	}
	if afterOverlap.Revision != baseline.Revision+1 || (afterOverlap.Instructions != "writer A" && afterOverlap.Instructions != "writer B") {
		t.Fatalf("overlapping writer state: revision=%d instructions=%q", afterOverlap.Revision, afterOverlap.Instructions)
	}

	fresh := httptest.NewRecorder()
	testHandler.UpdateAgent(fresh, withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"instructions":      "rebuilt from winning writer",
		"expected_revision": afterOverlap.Revision,
	}), "id", agentID))
	if fresh.Code != http.StatusOK {
		t.Fatalf("rebuilt writer: expected 200, got %d: %s", fresh.Code, fresh.Body.String())
	}
	afterFresh, err := testHandler.Queries.GetAgent(context.Background(), parseUUID(agentID))
	if err != nil {
		t.Fatalf("reload after fresh writer: %v", err)
	}
	if afterFresh.Revision != afterOverlap.Revision+1 || afterFresh.Instructions != "rebuilt from winning writer" {
		t.Fatalf("fresh writer state: revision=%d instructions=%q", afterFresh.Revision, afterFresh.Instructions)
	}
}

func TestUpdateAgentRejectsMixedCaseInstructionsKey(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "revision-cas-case-agent", nil)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"Instructions":      "bypass",
		"expected_revision": 1,
	}), "id", agentID))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("mixed-case instructions: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAutopilotDescriptionRevisionCAS(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createWebhookTestAgent(t, "revision-cas-autopilot-agent")
	autopilotID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	baseline, err := testHandler.Queries.GetAutopilot(context.Background(), parseUUID(autopilotID))
	if err != nil {
		t.Fatalf("load baseline autopilot: %v", err)
	}

	type result struct {
		code int
		body string
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for _, text := range []string{"writer A", "writer B"} {
		go func(text string) {
			<-start
			w := httptest.NewRecorder()
			testHandler.UpdateAutopilot(w, withURLParam(newRequest(http.MethodPatch, "/api/autopilots/"+autopilotID, map[string]any{
				"description":       text,
				"expected_revision": baseline.Revision,
			}), "id", autopilotID))
			results <- result{code: w.Code, body: w.Body.String()}
		}(text)
	}
	close(start)
	var successes, conflicts int
	for range 2 {
		got := <-results
		switch {
		case got.code == http.StatusOK:
			successes++
		case got.code == http.StatusConflict && strings.Contains(got.body, "revision_conflict"):
			conflicts++
		default:
			t.Fatalf("overlapping writer: unexpected response %d: %s", got.code, got.body)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("overlapping writers: successes=%d conflicts=%d, want one each", successes, conflicts)
	}

	afterOverlap, err := testHandler.Queries.GetAutopilot(context.Background(), parseUUID(autopilotID))
	if err != nil {
		t.Fatalf("reload after overlapping writers: %v", err)
	}
	if afterOverlap.Revision != baseline.Revision+1 || (afterOverlap.Description.String != "writer A" && afterOverlap.Description.String != "writer B") {
		t.Fatalf("overlapping writer state: revision=%d description=%q", afterOverlap.Revision, afterOverlap.Description.String)
	}

	fresh := httptest.NewRecorder()
	testHandler.UpdateAutopilot(fresh, withURLParam(newRequest(http.MethodPatch, "/api/autopilots/"+autopilotID, map[string]any{
		"description":       "rebuilt from winning writer",
		"expected_revision": afterOverlap.Revision,
	}), "id", autopilotID))
	if fresh.Code != http.StatusOK {
		t.Fatalf("rebuilt writer: expected 200, got %d: %s", fresh.Code, fresh.Body.String())
	}
	afterFresh, err := testHandler.Queries.GetAutopilot(context.Background(), parseUUID(autopilotID))
	if err != nil {
		t.Fatalf("reload after fresh writer: %v", err)
	}
	if afterFresh.Revision != afterOverlap.Revision+1 || afterFresh.Description.String != "rebuilt from winning writer" {
		t.Fatalf("fresh writer state: revision=%d description=%q", afterFresh.Revision, afterFresh.Description.String)
	}
}

func TestUpdateAutopilotRejectsMixedCaseDescriptionKey(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createWebhookTestAgent(t, "revision-cas-description-case-agent")
	autopilotID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	w := httptest.NewRecorder()
	testHandler.UpdateAutopilot(w, withURLParam(newRequest(http.MethodPatch, "/api/autopilots/"+autopilotID, map[string]any{
		"Description":       "bypass",
		"expected_revision": 1,
	}), "id", autopilotID))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("mixed-case description: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPromptUpdatesWithoutRevisionRemainBackwardCompatible(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "revision-compat-agent", nil)
	agentW := httptest.NewRecorder()
	testHandler.UpdateAgent(agentW, withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"instructions": "legacy agent writer",
	}), "id", agentID))
	if agentW.Code != http.StatusOK {
		t.Fatalf("legacy agent prompt update: expected 200, got %d: %s", agentW.Code, agentW.Body.String())
	}

	webhookAgentID := createWebhookTestAgent(t, "revision-compat-autopilot-agent")
	autopilotID := createWebhookTestAutopilot(t, webhookAgentID, "active", "run_only")
	autopilotW := httptest.NewRecorder()
	testHandler.UpdateAutopilot(autopilotW, withURLParam(newRequest(http.MethodPatch, "/api/autopilots/"+autopilotID, map[string]any{
		"description": "legacy autopilot writer",
	}), "id", autopilotID))
	if autopilotW.Code != http.StatusOK {
		t.Fatalf("legacy autopilot prompt update: expected 200, got %d: %s", autopilotW.Code, autopilotW.Body.String())
	}
}
