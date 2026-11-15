package sdp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/s2gamb/sdp-go"
)

// Service wraps sdp-go clients for identity lookup (via service token) and per-technician ticket queries.
type Service struct {
	baseURL       string
	telegramField string
	httpClient    *http.Client
	serviceClient *sdp.Client
}

// NewService creates a new SupportCenter Plus service wrapper.
func NewService(baseURL, serviceToken, telegramField string, insecureTLS bool) (*Service, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, errors.New("sdp: baseURL is required")
	}
	if serviceToken == "" {
		return nil, errors.New("sdp: serviceToken is required")
	}
	if telegramField == "" {
		return nil, errors.New("sdp: telegramField is required")
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if insecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // Explicitly requested via SDP_INSECURE_TLS for on-prem self-signed certs
	}

	httpClient := &http.Client{
		Timeout:   15 * time.Second,
		Transport: transport,
	}

	svcClient, err := sdp.NewClient(
		baseURL,
		sdp.WithAuthToken(serviceToken),
		sdp.WithHTTPClient(httpClient),
	)
	if err != nil {
		return nil, fmt.Errorf("sdp: init service client: %w", err)
	}

	return &Service{
		baseURL:       baseURL,
		telegramField: telegramField,
		httpClient:    httpClient,
		serviceClient: svcClient,
	}, nil
}

// ClientForToken creates an sdp.Client authenticated with a specific technician's authtoken.
func (s *Service) ClientForToken(userToken string) (*sdp.Client, error) {
	return sdp.NewClient(
		s.baseURL,
		sdp.WithAuthToken(userToken),
		sdp.WithHTTPClient(s.httpClient),
	)
}

// FindTechnicianByTelegramID looks up a technician in SupportCenter Plus whose configured
// Telegram User ID field matches telegramUserID. Uses the service token.
func (s *Service) FindTechnicianByTelegramID(ctx context.Context, telegramUserID int64) (*sdp.DefaultTechnician, error) {
	val := strconv.FormatInt(telegramUserID, 10)
	return s.serviceClient.Technicians.FindByField(ctx, s.telegramField, val)
}

// ValidateTechnicianToken verifies that a technician's authtoken is valid by executing
// a minimal read request against SupportCenter Plus.
func (s *Service) ValidateTechnicianToken(ctx context.Context, userToken string) error {
	cli, err := s.ClientForToken(userToken)
	if err != nil {
		return err
	}
	_, err = cli.Requests.List(ctx, &sdp.ListOptions{
		Page:     1,
		RowCount: 1,
	})
	return err
}

// TechnicianGroupMapping represents a technician's membership in a support group.
type TechnicianGroupMapping struct {
	SDPTechnicianID  string
	SupportGroupID   string
	SupportGroupName string
}

// FetchTechnicianGroups fetches all non-deleted support groups via the service token
// and returns the technician-to-group memberships.
func (s *Service) FetchTechnicianGroups(ctx context.Context) ([]TechnicianGroupMapping, error) {
	var mappings []TechnicianGroupMapping
	page := 1
	const rowCount = 100

	for {
		listResp, err := s.serviceClient.SupportGroups.List(ctx, &sdp.ListOptions{
			Page:     page,
			RowCount: rowCount,
		})
		if err != nil {
			return nil, fmt.Errorf("list support groups (page %d): %w", page, err)
		}

		for _, g := range listResp.SupportGroups {
			if g.Deleted || g.ID == "" {
				continue
			}
			detail, err := s.serviceClient.SupportGroups.Get(ctx, g.ID)
			if err != nil {
				return nil, fmt.Errorf("get support group %s (%s): %w", g.ID, g.Name, err)
			}
			if detail.Deleted {
				continue
			}
			for _, tech := range detail.Technicians {
				if tech.ID == "" {
					continue
				}
				mappings = append(mappings, TechnicianGroupMapping{
					SDPTechnicianID:  tech.ID,
					SupportGroupID:   detail.ID,
					SupportGroupName: detail.Name,
				})
			}
		}

		if !listResp.ListInfo.HasMoreRows {
			break
		}
		page++
	}

	return mappings, nil
}

// ListRequests queries tickets using the technician's own authtoken so SupportCenter enforces effective RBAC.
func (s *Service) ListRequests(
	ctx context.Context,
	userToken string,
	filter FilterKind,
	techID string,
	groupIDs []string,
	page, perPage int,
) (*sdp.RequestListResponse[map[string]any], error) {
	if filter == FilterMyGroups && len(groupIDs) == 0 {
		return &sdp.RequestListResponse[map[string]any]{}, nil
	}

	cli, err := s.ClientForToken(userToken)
	if err != nil {
		return nil, err
	}

	opts := BuildListOptions(filter, techID, groupIDs, page, perPage)
	return cli.Requests.List(ctx, opts)
}

// GetRequest fetches a single ticket by ID using the technician's own authtoken.
func (s *Service) GetRequest(ctx context.Context, userToken string, ticketID string) (*sdp.DefaultRequest, error) {
	cli, err := s.ClientForToken(userToken)
	if err != nil {
		return nil, err
	}

	return cli.Requests.Get(ctx, ticketID)
}

// TicketWebURL returns the direct SupportCenter Plus web UI URL for a ticket ID.
func (s *Service) TicketWebURL(ticketID string) string {
	return fmt.Sprintf("%s/WorkOrder.do?woMode=viewWO&woID=%s", s.baseURL, url.QueryEscape(ticketID))
}

// IsAuthError reports whether err is a SupportCenter authentication or authorization error.
func IsAuthError(err error) bool {
	var apiErr *sdp.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.HTTPStatusCode == http.StatusUnauthorized || apiErr.HTTPStatusCode == http.StatusForbidden {
		return true
	}
	for _, st := range apiErr.ResponseStatus {
		for _, m := range st.Messages {
			msg := strings.ToLower(m.Message)
			if strings.Contains(msg, "unable to authenticate") ||
				strings.Contains(msg, "support rep key") ||
				strings.Contains(msg, "invalid authtoken") ||
				strings.Contains(msg, "authentication failed") {
				return true
			}
		}
	}
	return false
}
