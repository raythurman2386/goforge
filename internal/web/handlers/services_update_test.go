package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestServiceHandler_UpdateImage(t *testing.T) {
	tests := []struct {
		name        string
		user        string
		status      models.ServiceInstanceStatus
		wantCode    int
		wantPending bool
	}{
		{"owner updates", "owner", models.ServiceStatusRunning, http.StatusOK, true},
		{"other user forbidden", "intruder", models.ServiceStatusRunning, http.StatusForbidden, false},
		{"busy instance conflicts", "owner", models.ServiceStatusPending, http.StatusConflict, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, repo := newEditHandler(t, tt.status)
			rr := httptest.NewRecorder()
			h.UpdateImage(rr, editRequest(http.MethodPost, tt.user, nil))

			if rr.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d (body %q)", rr.Code, tt.wantCode, rr.Body.String())
			}
			repo.mu.Lock()
			defer repo.mu.Unlock()
			if !tt.wantPending {
				if len(repo.saw) != 0 {
					t.Errorf("instance updated %d times, want 0", len(repo.saw))
				}
				return
			}
			if loc := rr.Header().Get("HX-Redirect"); loc != "/services/"+editTestInstanceID {
				t.Errorf("HX-Redirect = %q", loc)
			}
			if len(repo.saw) == 0 || repo.saw[0].Status != models.ServiceStatusPending {
				t.Fatalf("first saved status = %v, want pending", repo.saw)
			}
		})
	}
}

func TestServiceHandler_Show_HasUpdateButton(t *testing.T) {
	h, _ := newEditHandler(t, models.ServiceStatusRunning)
	rr := httptest.NewRecorder()
	h.Show(rr, editRequest(http.MethodGet, "owner", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if want := `hx-post="/services/` + editTestInstanceID + `/update"`; !strings.Contains(rr.Body.String(), want) {
		t.Errorf("detail page missing update button %s", want)
	}
}
