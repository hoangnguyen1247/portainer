package endpoints

import (
	"net/http"

	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/response"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/api/internal/set"
)

// @id AgentVersions
// @summary List agent versions
// @description List all agent versions based on the current user authorizations and query parameters.
// @description **Access policy**: restricted
// @tags endpoints
// @security ApiKeyAuth
// @security jwt
// @produce json
// @param search query string false "Search query"
// @param groupIds query []int false "List environments(endpoints) of these groups"
// @param status query []int false "List environments(endpoints) by this status"
// @param types query []int false "List environments(endpoints) of this type"
// @param tagIds query []int false "search environments(endpoints) with these tags (depends on tagsPartialMatch)"
// @param tagsPartialMatch query bool false "If true, will return environment(endpoint) which has one of tagIds, if false (or missing) will return only environments(endpoints) that has all the tags"
// @param endpointIds query []int false "will return only these environments(endpoints)"
// @param provisioned query bool false "If true, will return environment(endpoint) that were provisioned"
// @param agentVersions query []string false "will return only environments with on of these agent versions"
// @param edgeDeviceFilter query string false "will return only these edge environments, none will return only regular edge environments" Enum("all", "trusted", "untrusted", "none")
// @param name query string false "will return only environments(endpoints) with this name"
// @success 200 {array} string "List of available agent versions"
// @failure 500 "Server error"
// @router /endpoints/agent_versions [get]

func (handler *Handler) agentVersions(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	endpointGroups, err := handler.DataStore.EndpointGroup().EndpointGroups()
	if err != nil {
		return httperror.InternalServerError("Unable to retrieve environment groups from the database", err)
	}

	endpoints, err := handler.DataStore.Endpoint().Endpoints()
	if err != nil {
		return httperror.InternalServerError("Unable to retrieve environments from the database", err)
	}

	settings, err := handler.DataStore.Settings().Settings()
	if err != nil {
		return httperror.InternalServerError("Unable to retrieve settings from the database", err)
	}

	securityContext, err := security.RetrieveRestrictedRequestContext(r)
	if err != nil {
		return httperror.InternalServerError("Unable to retrieve info from request context", err)
	}

	query, err := parseQuery(r)
	if err != nil {
		return httperror.BadRequest("Invalid query parameters", err)
	}

	filteredEndpoints := security.FilterEndpoints(endpoints, endpointGroups, securityContext)

	filteredEndpoints, _, err = handler.filterEndpointsByQuery(filteredEndpoints, query, endpointGroups, settings)
	if err != nil {
		return httperror.InternalServerError("Unable to filter endpoints", err)
	}

	agentVersions := set.Set[string]{}
	for _, endpoint := range filteredEndpoints {
		if endpoint.Agent.Version != "" {
			agentVersions[endpoint.Agent.Version] = true
		}
	}

	return response.JSON(w, agentVersions.Keys())
}
