// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

// Package commands implements the lfx CLI subcommands.
package commands

import (
	queryhttpclient "github.com/linuxfoundation/lfx-v2-query-service/gen/http/query_svc/client"
	"github.com/urfave/cli/v3"
	goahttp "goa.design/goa/v3/http"
)

// queryService describes the LFX v2 query service.
var queryService = goaService[*queryhttpclient.Client]{
	Name:  "query-svc",
	Usage: "Query LFX v2 resources and organizations via the query service",
	NewClient: func(scheme, host string, doer goahttp.Doer) *queryhttpclient.Client {
		return queryhttpclient.NewClient(scheme, host, doer, goahttp.RequestEncoder, goahttp.ResponseDecoder, false)
	},
	Examples: []goaExample{
		{
			Description: "Find a project by its slug, printing its entity ID (project:<project_uid>).",
			Command:     "lfx query-svc query-resources --type project --filters-all slug:tlf -q 'resources.0.id'",
		},
		{
			Description: "Number of child projects for a given project.",
			Command:     "lfx query-svc query-resources-count --type project --parent <entity-id>",
		},
		{
			Description: `Find committees with "board" in the name for a given project.`,
			Command:     "lfx query-svc query-resources --type committee --parent <entity-id> --name board",
		},
	},
}

// NewQueryServiceCommand builds the `lfx query-svc` command group.
func NewQueryServiceCommand() *cli.Command {
	return queryService.command(queryServiceEndpoints())
}

// queryVersionArg returns the API version argument for a query-svc builder.
func queryVersionArg(param string) goaBuilderArg {
	return goaBuilderArg{Param: param, Kind: goaArgString, Flag: "version", Usage: "Version of the API", Default: "1"}
}

// queryServiceEndpoints lists the query-svc endpoints exposed by lfx. Each
// Args list mirrors its builder's parameters in signature order; see
// goaBuilderArg.
func queryServiceEndpoints() []goaEndpointSpec[*queryhttpclient.Client] {
	return []goaEndpointSpec[*queryhttpclient.Client]{
		{
			Name:     "query-resources",
			Usage:    "Locate resources by their type or parent, or search by display name or alias",
			Builder:  queryhttpclient.BuildQueryResourcesPayload,
			Endpoint: (*queryhttpclient.Client).QueryResources,
			Args: []goaBuilderArg{
				queryVersionArg("querySvcQueryResourcesVersion"),
				{Param: "querySvcQueryResourcesName", Kind: goaArgString, Flag: "name", Usage: "Resource name or alias; supports typeahead"},
				{Param: "querySvcQueryResourcesParent", Kind: goaArgString, Flag: "parent", Usage: "Parent reference, e.g. project:<uid>"},
				{Param: "querySvcQueryResourcesType", Kind: goaArgString, Flag: "type", Usage: "Resource type to search"},
				{Param: "querySvcQueryResourcesTags", Kind: goaArgJSONList, Flag: "tags", Usage: "Tag to match with OR logic"},
				{Param: "querySvcQueryResourcesTagsAll", Kind: goaArgJSONList, Flag: "tags-all", Usage: "Tag to match with AND logic"},
				{Param: "querySvcQueryResourcesDateField", Kind: goaArgString, Flag: "date-field", Usage: "Data field to filter by date (requires --date-from or --date-to)"},
				{Param: "querySvcQueryResourcesDateFrom", Kind: goaArgString, Flag: "date-from", Usage: "Inclusive start, ISO 8601 date or datetime"},
				{Param: "querySvcQueryResourcesDateTo", Kind: goaArgString, Flag: "date-to", Usage: "Inclusive end, ISO 8601 date or datetime"},
				{Param: "querySvcQueryResourcesFilters", Kind: goaArgJSONList, Flag: "filters", Usage: "Data field filter as 'field:value'"},
				{Param: "querySvcQueryResourcesFiltersAll", Kind: goaArgJSONList, Flag: "filters-all", Usage: "Data field filter as 'field:value', all must match"},
				{Param: "querySvcQueryResourcesFiltersOr", Kind: goaArgJSONList, Flag: "filters-or", Usage: "Data field filter as 'field:value', at least one must match"},
				{Param: "querySvcQueryResourcesCelFilter", Kind: goaArgString, Flag: "cel-filter", Usage: "CEL expression over data, resource_type, and id"},
				{Param: "querySvcQueryResourcesFilterGrants", Kind: goaArgString, Flag: "filter-grants", Usage: "Only resources with direct FGA grants ('direct'); requires --type"},
				{Param: "querySvcQueryResourcesSort", Kind: goaArgString, Flag: "sort", Usage: "Sort order: name_asc, name_desc, updated_asc, updated_desc, best_match", Default: "name_asc"},
				{Param: "querySvcQueryResourcesPageToken", Kind: goaArgString, Flag: "page-token", Usage: "Opaque token for the next page"},
				{Param: "querySvcQueryResourcesPageSize", Kind: goaArgString, Flag: "page-size", Usage: "Number of results per page (1-1000)", Default: "50"},
				{Param: "querySvcQueryResourcesBearerToken", Kind: goaArgBearerToken},
			},
		},
		{
			Name:     "query-resources-count",
			Usage:    "Count matching resources, optionally grouped by tag or with a cardinality metric",
			Builder:  queryhttpclient.BuildQueryResourcesCountPayload,
			Endpoint: (*queryhttpclient.Client).QueryResourcesCount,
			Args: []goaBuilderArg{
				queryVersionArg("querySvcQueryResourcesCountVersion"),
				{Param: "querySvcQueryResourcesCountName", Kind: goaArgString, Flag: "name", Usage: "Resource name or alias; supports typeahead"},
				{Param: "querySvcQueryResourcesCountParent", Kind: goaArgString, Flag: "parent", Usage: "Parent reference, e.g. project:<uid>"},
				{Param: "querySvcQueryResourcesCountType", Kind: goaArgString, Flag: "type", Usage: "Resource type to count"},
				{Param: "querySvcQueryResourcesCountTags", Kind: goaArgJSONList, Flag: "tags", Usage: "Tag to match with OR logic"},
				{Param: "querySvcQueryResourcesCountTagsAll", Kind: goaArgJSONList, Flag: "tags-all", Usage: "Tag to match with AND logic"},
				{Param: "querySvcQueryResourcesCountDateField", Kind: goaArgString, Flag: "date-field", Usage: "Data field to filter by date (requires --date-from or --date-to)"},
				{Param: "querySvcQueryResourcesCountDateFrom", Kind: goaArgString, Flag: "date-from", Usage: "Inclusive start, ISO 8601 date or datetime"},
				{Param: "querySvcQueryResourcesCountDateTo", Kind: goaArgString, Flag: "date-to", Usage: "Inclusive end, ISO 8601 date or datetime"},
				{Param: "querySvcQueryResourcesCountFilters", Kind: goaArgJSONList, Flag: "filters", Usage: "Data field filter as 'field:value'"},
				{Param: "querySvcQueryResourcesCountFiltersAll", Kind: goaArgJSONList, Flag: "filters-all", Usage: "Data field filter as 'field:value', all must match"},
				{Param: "querySvcQueryResourcesCountFiltersOr", Kind: goaArgJSONList, Flag: "filters-or", Usage: "Data field filter as 'field:value', at least one must match"},
				{Param: "querySvcQueryResourcesCountGroupBy", Kind: goaArgString, Flag: "group-by", Usage: "Tag prefix to group the count by"},
				{Param: "querySvcQueryResourcesCountGroupBySize", Kind: goaArgString, Flag: "group-by-size", Usage: "Maximum number of groups (1-1000); requires --group-by"},
				{Param: "querySvcQueryResourcesCountMetric", Kind: goaArgString, Flag: "metric", Usage: "Metric to compute, e.g. cardinality:<tag_prefix>"},
				{Param: "querySvcQueryResourcesCountBearerToken", Kind: goaArgBearerToken},
			},
		},
		{
			Name:     "query-membership-summary",
			Usage:    "Summarize membership records per organization and project",
			Builder:  queryhttpclient.BuildQueryMembershipSummaryPayload,
			Endpoint: (*queryhttpclient.Client).QueryMembershipSummary,
			Args: []goaBuilderArg{
				queryVersionArg("querySvcQueryMembershipSummaryVersion"),
				{Param: "querySvcQueryMembershipSummaryProjectUID", Kind: goaArgString, Flag: "project-uid", Usage: "Project UID to summarize"},
				{Param: "querySvcQueryMembershipSummaryB2bOrgUID", Kind: goaArgString, Flag: "b2b-org-uid", Usage: "B2B organization UID to summarize"},
				{Param: "querySvcQueryMembershipSummaryPageToken", Kind: goaArgString, Flag: "page-token", Usage: "Opaque token for the next page"},
				{Param: "querySvcQueryMembershipSummaryBearerToken", Kind: goaArgBearerToken},
			},
		},
		{
			Name:     "query-orgs",
			Usage:    "Locate a single organization by name or domain",
			Builder:  queryhttpclient.BuildQueryOrgsPayload,
			Endpoint: (*queryhttpclient.Client).QueryOrgs,
			Args: []goaBuilderArg{
				queryVersionArg("querySvcQueryOrgsVersion"),
				{Param: "querySvcQueryOrgsName", Kind: goaArgString, Flag: "name", Usage: "Organization name"},
				{Param: "querySvcQueryOrgsDomain", Kind: goaArgString, Flag: "domain", Usage: "Organization domain name, e.g. example.com (no scheme or path)"},
				{Param: "querySvcQueryOrgsBearerToken", Kind: goaArgBearerToken},
			},
		},
		{
			Name:     "suggest-orgs",
			Usage:    "Get organization suggestions for a typeahead query",
			Builder:  queryhttpclient.BuildSuggestOrgsPayload,
			Endpoint: (*queryhttpclient.Client).SuggestOrgs,
			Args: []goaBuilderArg{
				queryVersionArg("querySvcSuggestOrgsVersion"),
				{Param: "querySvcSuggestOrgsQuery", Kind: goaArgString, Flag: "search", Usage: "Search query (required)"},
				{Param: "querySvcSuggestOrgsBearerToken", Kind: goaArgBearerToken},
			},
		},
	}
}
