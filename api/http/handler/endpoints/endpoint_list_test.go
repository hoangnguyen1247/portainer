package endpoints

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/datastore"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/api/internal/testhelpers"
	helper "github.com/portainer/portainer/api/internal/testhelpers"
	"github.com/stretchr/testify/assert"
)

type endpointListTest struct {
	title    string
	expected []portainer.EndpointID
}

func Test_EndpointList_AgentVersion(t *testing.T) {

	version1Endpoint := portainer.Endpoint{ID: 1, GroupID: 1, Agent: struct {
		Version string "example:\"1.0.0\""
	}{Version: "1.0.0"}}
	version2Endpoint := portainer.Endpoint{ID: 2, GroupID: 1, Agent: struct {
		Version string "example:\"1.0.0\""
	}{Version: "2.0.0"}}
	noVersionEndpoint := portainer.Endpoint{ID: 3, GroupID: 1}

	handler, teardown := setup(t, []portainer.Endpoint{
		version1Endpoint,
		version2Endpoint,
		noVersionEndpoint,
	})

	defer teardown()

	type endpointListAgentVersionTest struct {
		endpointListTest
		filter []string
	}

	tests := []endpointListAgentVersionTest{
		{
			endpointListTest{
				"should show version 1 endpoints",
				[]portainer.EndpointID{version1Endpoint.ID},
			},
			[]string{version1Endpoint.Agent.Version},
		},
		{
			endpointListTest{
				"should show version 2 endpoints",
				[]portainer.EndpointID{version2Endpoint.ID},
			},
			[]string{version2Endpoint.Agent.Version},
		},
		{
			endpointListTest{
				"should show version 1 and 2 endpoints",
				[]portainer.EndpointID{version2Endpoint.ID, version1Endpoint.ID},
			},
			[]string{version2Endpoint.Agent.Version, version1Endpoint.Agent.Version},
		},
	}

	for _, test := range tests {
		t.Run(test.title, func(t *testing.T) {
			is := assert.New(t)
			query := ""
			for _, filter := range test.filter {
				query += fmt.Sprintf("agentVersions[]=%s&", filter)
			}

			req := buildEndpointListRequest(query)

			resp, err := doEndpointListRequest(req, handler, is)
			is.NoError(err)

			is.Equal(len(test.expected), len(resp))

			respIds := []portainer.EndpointID{}

			for _, endpoint := range resp {
				respIds = append(respIds, endpoint.ID)
			}

			is.ElementsMatch(test.expected, respIds)
		})
	}

}

func Test_endpointList_edgeDeviceFilter(t *testing.T) {

	trustedEndpoint := portainer.Endpoint{ID: 1, UserTrusted: true, IsEdgeDevice: true, GroupID: 1, Type: portainer.EdgeAgentOnDockerEnvironment}
	untrustedEndpoint := portainer.Endpoint{ID: 2, UserTrusted: false, IsEdgeDevice: true, GroupID: 1, Type: portainer.EdgeAgentOnDockerEnvironment}
	regularUntrustedEdgeEndpoint := portainer.Endpoint{ID: 3, UserTrusted: false, IsEdgeDevice: false, GroupID: 1, Type: portainer.EdgeAgentOnDockerEnvironment}
	regularTrustedEdgeEndpoint := portainer.Endpoint{ID: 4, UserTrusted: true, IsEdgeDevice: false, GroupID: 1, Type: portainer.EdgeAgentOnDockerEnvironment}
	regularEndpoint := portainer.Endpoint{ID: 5, UserTrusted: false, IsEdgeDevice: false, GroupID: 1, Type: portainer.DockerEnvironment}

	handler, teardown := setup(t, []portainer.Endpoint{
		trustedEndpoint,
		untrustedEndpoint,
		regularUntrustedEdgeEndpoint,
		regularTrustedEdgeEndpoint,
		regularEndpoint,
	})

	defer teardown()

	type endpointListEdgeDeviceTest struct {
		endpointListTest
		filter EdgeDeviceFilter
	}

	tests := []endpointListEdgeDeviceTest{
		{
			endpointListTest{
				"should show all edge endpoints",
				[]portainer.EndpointID{trustedEndpoint.ID, untrustedEndpoint.ID, regularUntrustedEdgeEndpoint.ID, regularTrustedEdgeEndpoint.ID},
			},
			EdgeDeviceFilterAll,
		},
		{
			endpointListTest{
				"should show only trusted edge devices",
				[]portainer.EndpointID{trustedEndpoint.ID, regularTrustedEdgeEndpoint.ID},
			},
			EdgeDeviceFilterTrusted,
		},
		{
			endpointListTest{
				"should show only untrusted edge devices",
				[]portainer.EndpointID{untrustedEndpoint.ID, regularUntrustedEdgeEndpoint.ID},
			},
			EdgeDeviceFilterUntrusted,
		},
		{
			endpointListTest{
				"should show no edge devices",
				[]portainer.EndpointID{regularEndpoint.ID, regularUntrustedEdgeEndpoint.ID, regularTrustedEdgeEndpoint.ID},
			},
			EdgeDeviceFilterNone,
		},
	}

	for _, test := range tests {
		t.Run(test.title, func(t *testing.T) {
			is := assert.New(t)

			req := buildEndpointListRequest(fmt.Sprintf("edgeDeviceFilter=%s", test.filter))
			resp, err := doEndpointListRequest(req, handler, is)
			is.NoError(err)

			is.Equal(len(test.expected), len(resp))

			respIds := []portainer.EndpointID{}

			for _, endpoint := range resp {
				respIds = append(respIds, endpoint.ID)
			}

			is.ElementsMatch(test.expected, respIds)
		})
	}
}

func setup(t *testing.T, endpoints []portainer.Endpoint) (handler *Handler, teardown func()) {
	is := assert.New(t)
	_, store, teardown := datastore.MustNewTestStore(true, true)

	for _, endpoint := range endpoints {
		err := store.Endpoint().Create(&endpoint)
		is.NoError(err, "error creating environment")
	}

	err := store.User().Create(&portainer.User{Username: "admin", Role: portainer.AdministratorRole})
	is.NoError(err, "error creating a user")

	bouncer := helper.NewTestRequestBouncer()
	handler = NewHandler(bouncer, nil)
	handler.DataStore = store
	handler.ComposeStackManager = testhelpers.NewComposeStackManager()

	return handler, teardown
}

func buildEndpointListRequest(query string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/endpoints?%s", query), nil)

	ctx := security.StoreTokenData(req, &portainer.TokenData{ID: 1, Username: "admin", Role: 1})
	req = req.WithContext(ctx)

	restrictedCtx := security.StoreRestrictedRequestContext(req, &security.RestrictedRequestContext{UserID: 1, IsAdmin: true})
	req = req.WithContext(restrictedCtx)

	req.Header.Add("Authorization", "Bearer dummytoken")

	return req
}

func doEndpointListRequest(req *http.Request, h *Handler, is *assert.Assertions) ([]portainer.Endpoint, error) {
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	is.Equal(http.StatusOK, rr.Code, "Status should be 200")
	body, err := io.ReadAll(rr.Body)
	if err != nil {
		return nil, err
	}

	resp := []portainer.Endpoint{}
	err = json.Unmarshal(body, &resp)
	if err != nil {
		return nil, err
	}

	return resp, nil
}
