package sdp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/s2gamb/sdp-go"
)

func TestBuildListOptions(t *testing.T) {
	optsMy := BuildListOptions(FilterMyOpen, "99", nil, 2, 10)
	if optsMy.Page != 2 || optsMy.RowCount != 10 {
		t.Fatalf("unexpected pagination: %+v", optsMy)
	}
	if len(optsMy.SearchCriteria) != 4 || optsMy.SearchCriteria[0].Field != "technician.id" || optsMy.SearchCriteria[0].Value != "99" {
		t.Fatalf("unexpected MyOpen criteria: %+v", optsMy.SearchCriteria)
	}
	for i, wantStatus := range []string{StatusClosed, StatusCompleted, StatusCancelled} {
		c := optsMy.SearchCriteria[i+1]
		if c.Field != "status.name" || c.Value != wantStatus || c.LogicalOperator != "and" {
			t.Fatalf("expected MyOpen status filter %q with LogicalOperator 'and', got %+v", wantStatus, c)
		}
	}

	optsUn := BuildListOptions(FilterUnassigned, "99", nil, 1, 8)
	if len(optsUn.SearchCriteria) != 4 || optsUn.SearchCriteria[0].Field != "technician.id" || optsUn.SearchCriteria[0].Value != nil {
		t.Fatalf("unexpected Unassigned criteria: %+v", optsUn.SearchCriteria)
	}

	optsGrp := BuildListOptions(FilterMyGroups, "99", []string{"301", "601"}, 0, 0)
	if optsGrp.Page != 1 || optsGrp.RowCount != 8 {
		t.Fatalf("expected clamped defaults page=1 rowCount=8, got page=%d rowCount=%d", optsGrp.Page, optsGrp.RowCount)
	}
	if len(optsGrp.SearchCriteria) != 5 {
		t.Fatalf("expected 5 MyGroups criteria (2 groups + 3 excluded statuses), got %+v", optsGrp.SearchCriteria)
	}
	if optsGrp.SearchCriteria[0].Field != "group.id" || optsGrp.SearchCriteria[0].Value != "301" || optsGrp.SearchCriteria[0].LogicalOperator != "" {
		t.Fatalf("unexpected MyGroups criteria[0]: %+v", optsGrp.SearchCriteria[0])
	}
	if optsGrp.SearchCriteria[1].Field != "group.id" || optsGrp.SearchCriteria[1].Value != "601" || optsGrp.SearchCriteria[1].LogicalOperator != "or" {
		t.Fatalf("unexpected MyGroups criteria[1]: %+v", optsGrp.SearchCriteria[1])
	}
	if optsGrp.SearchCriteria[2].Field != "status.name" || optsGrp.SearchCriteria[2].LogicalOperator != "and" {
		t.Fatalf("unexpected MyGroups criteria[2]: %+v", optsGrp.SearchCriteria[2])
	}

	if got := ParseFilterKind("unknown"); got != FilterMyOpen {
		t.Fatalf("expected fallback FilterMyOpen, got %q", got)
	}
	if len(AllFilters()) != 3 {
		t.Fatalf("expected 3 filters, got %d", len(AllFilters()))
	}
}

func TestService_MockSDPServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("authtoken")
		switch r.URL.Path {
		case "/api/v3/technicians":
			if token != "svc-token" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"response_status":{"status_code":4000,"status":"failed"}}`))
				return
			}
			_, _ = w.Write([]byte(`{
				"response_status":[{"status_code":2000,"status":"success"}],
				"technicians":[{"id":"42","name":"Alice Tech"}]
			}`))
		case "/api/v3/requests":
			if token != "user-token" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"response_status":{"status_code":4000,"status":"failed"}}`))
				return
			}
			_, _ = w.Write([]byte(`{
				"response_status":[{"status_code":2000,"status":"success"}],
				"list_info":{"has_more_rows":false,"page":1,"row_count":8},
				"requests":[{"id":"606","subject":"VPN issue"}]
			}`))
		case "/api/v3/requests/606":
			if token != "user-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{
				"response_status":{"status_code":2000,"status":"success"},
				"request":{"id":"606","subject":"VPN issue"}
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	svc, err := NewService(srv.URL, "svc-token", "udf_tg_id", false)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	ctx := context.Background()
	tech, err := svc.FindTechnicianByTelegramID(ctx, 123456)
	if err != nil {
		t.Fatalf("FindTechnicianByTelegramID failed: %v", err)
	}
	if tech.ID != "42" || tech.Name != "Alice Tech" {
		t.Fatalf("unexpected tech: %+v", tech)
	}

	if err := svc.ValidateTechnicianToken(ctx, "user-token"); err != nil {
		t.Fatalf("ValidateTechnicianToken valid failed: %v", err)
	}
	badErr := svc.ValidateTechnicianToken(ctx, "wrong-token")
	if badErr == nil || !IsAuthError(badErr) {
		t.Fatalf("expected IsAuthError for wrong token, got %v", badErr)
	}

	listResp, err := svc.ListRequests(ctx, "user-token", FilterMyOpen, "42", nil, 1, 8)
	if err != nil || len(listResp.Requests) != 1 || listResp.Requests[0].ID != "606" {
		t.Fatalf("ListRequests unexpected: %+v, err=%v", listResp, err)
	}

	// FilterMyGroups with no groups should return empty immediately without hitting /api/v3/requests
	emptyGrpResp, err := svc.ListRequests(ctx, "user-token", FilterMyGroups, "42", nil, 1, 8)
	if err != nil || len(emptyGrpResp.Requests) != 0 {
		t.Fatalf("expected empty response for MyGroups with 0 groups, got %+v, err=%v", emptyGrpResp, err)
	}

	req, err := svc.GetRequest(ctx, "user-token", "606")
	if err != nil || req.Subject != "VPN issue" {
		t.Fatalf("GetRequest unexpected: %+v, err=%v", req, err)
	}

	if gotURL := svc.TicketWebURL("606"); gotURL != srv.URL+"/WorkOrder.do?woMode=viewWO&woID=606" {
		t.Fatalf("unexpected TicketWebURL: %q", gotURL)
	}

	// Ensure sdp.APIError check works directly
	if !IsAuthError(&sdp.APIError{HTTPStatusCode: http.StatusForbidden}) {
		t.Fatal("expected 403 to be recognized as auth error")
	}
	if !IsAuthError(&sdp.APIError{
		HTTPStatusCode: http.StatusBadRequest,
		ResponseStatus: sdp.FlexibleResponseStatus{
			{
				StatusCode: 4000,
				Status:     "failed",
				Messages: []sdp.ResponseMessage{
					{Message: "Support Rep key in the request is invalid. Unable to authenticate."},
				},
			},
		},
	}) {
		t.Fatal("expected HTTP 400 invalid Support Rep key to be recognized as auth error")
	}
}
