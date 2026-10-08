package httpapi

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

func (r *Router) registerClientResourceRoutes() {
	r.mux.HandleFunc("GET /api/v1/client-resources", r.listClientResources)
	r.mux.HandleFunc("GET /api/v1/client-resources/{id}", r.getClientResourceSummary)
	r.mux.HandleFunc("POST /api/v1/locations", r.createLocation)
	r.mux.HandleFunc("POST /api/v1/contracts", r.createContract)
	r.mux.HandleFunc("POST /api/v1/contacts", r.createContact)
	r.mux.HandleFunc("POST /api/v1/services", r.createService)
	r.mux.HandleFunc("POST /api/v1/assets", r.createAsset)
	for _, resource := range []struct {
		plural string
		kind   clientresources.Kind
	}{
		{plural: "locations", kind: clientresources.LocationKind},
		{plural: "contacts", kind: clientresources.ContactKind},
		{plural: "assets", kind: clientresources.AssetKind},
		{plural: "services", kind: clientresources.ServiceKind},
		{plural: "contracts", kind: clientresources.ContractKind},
	} {
		plural, kind := resource.plural, resource.kind
		r.mux.HandleFunc("GET /api/v1/"+plural+"/{id}", func(w http.ResponseWriter, req *http.Request) { r.getClientResource(w, req, kind) })
		r.mux.HandleFunc("PATCH /api/v1/"+plural+"/{id}", func(w http.ResponseWriter, req *http.Request) { r.updateClientResource(w, req, kind) })
		r.mux.HandleFunc("POST /api/v1/"+plural+"/{id}/deactivate", func(w http.ResponseWriter, req *http.Request) { r.changeClientResourceLifecycle(w, req, kind, false) })
		r.mux.HandleFunc("POST /api/v1/"+plural+"/{id}/reactivate", func(w http.ResponseWriter, req *http.Request) { r.changeClientResourceLifecycle(w, req, kind, true) })
	}
}

func (r *Router) getClientResource(writer http.ResponseWriter, request *http.Request, kind clientresources.Kind) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	resourceID, ok := clientResourcePathID(writer, request)
	if !ok {
		return
	}
	if r.dependencies.ClientResourceQueries == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "client resource queries are unavailable")
		return
	}
	target := targetFor(principal)
	var result clientresources.ResourceDetail
	var err error
	if authorization.Authorize(principal, string(kind)+".lifecycle", target) == nil {
		result, err = r.dependencies.ClientResourceQueries.GetForLifecycle(request.Context(), clientresources.LifecycleGetCommand{
			Principal: principal, Target: target, Kind: kind, ID: resourceID,
		})
	} else {
		result, err = r.dependencies.ClientResourceQueries.Get(request.Context(), clientresources.GetCommand{
			Principal: principal, Target: target, Kind: kind, ID: resourceID,
		})
	}
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) updateClientResource(writer http.ResponseWriter, request *http.Request, kind clientresources.Kind) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	resourceID, ok := clientResourcePathID(writer, request)
	if !ok {
		return
	}
	actions := r.clientResourceLifecycle(kind)
	if actions == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "client resource lifecycle is unavailable")
		return
	}
	var body ClientResourceUpdateRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	if body.LocationID != nil && *body.LocationID != "" {
		locationID, err := uuid.Parse(*body.LocationID)
		if err != nil {
			writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "location_id must be a UUID")
			return
		}
		normalized := locationID.String()
		body.LocationID = &normalized
	}
	expected, ok := requiredClientResourceVersion(writer, request, body.ExpectedVersion)
	if !ok {
		return
	}
	result, err := actions.Update(request.Context(), clientresources.UpdateCommand{
		Principal: principal, Target: targetFor(principal), Kind: kind,
		ResourceID: resourceID, ExpectedVersion: expected,
		Patch: clientresources.UpdatePatch{
			Name: body.Name, DisplayName: body.DisplayName, Email: body.Email, Phone: body.Phone,
			LocationID: body.LocationID, AssetType: body.AssetType, Criticality: body.Criticality,
			StartsOn: body.StartsOn, EndsOn: body.EndsOn, ClearEndsOn: body.ClearEndsOn,
		},
		Reason: body.Reason, ActorID: principal.ID, Source: source(request), CorrelationID: uuid.NewString(),
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) changeClientResourceLifecycle(writer http.ResponseWriter, request *http.Request, kind clientresources.Kind, reactivate bool) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	resourceID, ok := clientResourcePathID(writer, request)
	if !ok {
		return
	}
	actions := r.clientResourceLifecycle(kind)
	if actions == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "client resource lifecycle is unavailable")
		return
	}
	var body ClientResourceLifecycleRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	expected, ok := requiredClientResourceVersion(writer, request, body.ExpectedVersion)
	if !ok {
		return
	}
	command := clientresources.LifecycleCommand{
		Principal: principal, Target: targetFor(principal), Kind: kind,
		ResourceID: resourceID, ExpectedVersion: expected,
		Reason: body.Reason, ActorID: principal.ID, Source: source(request), CorrelationID: uuid.NewString(),
	}
	var result clientresources.ResourceDetail
	var err error
	if reactivate {
		result, err = actions.Reactivate(request.Context(), command)
	} else {
		result, err = actions.Deactivate(request.Context(), command)
	}
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}

func clientResourcePathID(writer http.ResponseWriter, request *http.Request) (string, bool) {
	resourceID, err := uuid.Parse(request.PathValue("id"))
	if err != nil {
		writeError(request.Context(), writer, http.StatusNotFound, "not_found", "resource not found")
		return "", false
	}
	return resourceID.String(), true
}

func (r *Router) clientResourceLifecycle(kind clientresources.Kind) ClientResourceLifecycleActions {
	switch kind {
	case clientresources.LocationKind:
		return r.dependencies.LocationLifecycle
	case clientresources.ContactKind:
		return r.dependencies.ContactLifecycle
	case clientresources.AssetKind:
		return r.dependencies.AssetLifecycle
	case clientresources.ServiceKind:
		return r.dependencies.ServiceLifecycle
	case clientresources.ContractKind:
		return r.dependencies.ContractLifecycle
	default:
		return nil
	}
}

func requiredClientResourceVersion(writer http.ResponseWriter, request *http.Request, bodyVersion *int64) (int64, bool) {
	header, present, err := parseExpectedVersionETag(request)
	if err != nil || !present || (bodyVersion != nil && (*bodyVersion < 1 || *bodyVersion != header)) {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "a matching quoted If-Match version is required")
		return 0, false
	}
	return header, true
}

func (r *Router) getClientResourceSummary(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.ClientResourceCatalog == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "client resource catalog is unavailable")
		return
	}
	result, err := r.dependencies.ClientResourceCatalog.GetSummary(request.Context(), clientresources.GetCatalogCommand{
		Principal: principal, Target: targetFor(principal), ID: request.PathValue("id"),
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) listClientResources(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.ClientResourceCatalog == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "client resource catalog is unavailable")
		return
	}
	limit := 500
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 500 {
			writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "limit must be between 1 and 500")
			return
		}
		limit = parsed
	}
	lifecycle := clientresources.CatalogLifecycle(request.URL.Query().Get("lifecycle"))
	if lifecycle == "" {
		lifecycle = clientresources.CatalogActive
	}
	if lifecycle != clientresources.CatalogActive &&
		lifecycle != clientresources.CatalogInactive &&
		lifecycle != clientresources.CatalogAll {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "lifecycle must be active, inactive, or all")
		return
	}
	result, err := r.dependencies.ClientResourceCatalog.List(
		request.Context(),
		clientresources.ListCatalogCommand{
			Principal: principal, Target: targetFor(principal), Limit: limit, Lifecycle: lifecycle,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) createAsset(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(
			request.Context(), writer, http.StatusUnauthorized,
			"unauthenticated", "authentication required",
		)
		return
	}
	if r.dependencies.Assets == nil {
		writeError(
			request.Context(), writer, http.StatusNotImplemented,
			"not_implemented", "asset service is unavailable",
		)
		return
	}
	var body CreateAssetRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	if body.LocationID != "" && uuid.Validate(body.LocationID) != nil {
		writeError(
			request.Context(), writer, http.StatusUnprocessableEntity,
			"validation_failed", "location_id must be a UUID",
		)
		return
	}
	target := targetFor(principal)
	location := clientresources.Ref{}
	if body.LocationID != "" {
		location = clientresources.Ref{
			ID: body.LocationID, MSPID: target.MSPID, ClientID: target.ClientID,
		}
	}
	result, err := r.dependencies.Assets.CreateAsset(
		request.Context(),
		clientresources.CreateAssetCommand{
			Principal:            principal,
			Target:               target,
			Location:             location,
			DisplayID:            body.DisplayID,
			Name:                 body.Name,
			AssetType:            body.AssetType,
			SourceSystem:         body.SourceSystem,
			ExternalID:           body.ExternalID,
			Authority:            body.Authority,
			ActorID:              principal.ID,
			Source:               source(request),
			TagIDs:               body.TagIDs,
			ClassificationPolicy: tagging.CreationRequireMeaningful,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) createService(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(
			request.Context(), writer, http.StatusUnauthorized,
			"unauthenticated", "authentication required",
		)
		return
	}
	if r.dependencies.Services == nil {
		writeError(
			request.Context(), writer, http.StatusNotImplemented,
			"not_implemented", "service resource service is unavailable",
		)
		return
	}
	var body CreateServiceRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.Services.CreateService(
		request.Context(),
		clientresources.CreateServiceCommand{
			Principal:   principal,
			Target:      targetFor(principal),
			DisplayID:   body.DisplayID,
			Name:        body.Name,
			Criticality: body.Criticality,
			ActorID:     principal.ID,
			Source:      source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) createContact(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(
			request.Context(), writer, http.StatusUnauthorized,
			"unauthenticated", "authentication required",
		)
		return
	}
	if r.dependencies.Contacts == nil {
		writeError(
			request.Context(), writer, http.StatusNotImplemented,
			"not_implemented", "contact service is unavailable",
		)
		return
	}
	var body CreateContactRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	if body.LocationID != "" && uuid.Validate(body.LocationID) != nil {
		writeError(
			request.Context(), writer, http.StatusUnprocessableEntity,
			"validation_failed", "location_id must be a UUID",
		)
		return
	}
	target := targetFor(principal)
	location := clientresources.Ref{}
	if body.LocationID != "" {
		location = clientresources.Ref{
			ID: body.LocationID, MSPID: target.MSPID, ClientID: target.ClientID,
		}
	}
	result, err := r.dependencies.Contacts.CreateContact(
		request.Context(),
		clientresources.CreateContactCommand{
			Principal:   principal,
			Target:      target,
			Location:    location,
			DisplayID:   body.DisplayID,
			DisplayName: body.DisplayName,
			Email:       body.Email,
			Phone:       body.Phone,
			ActorID:     principal.ID,
			Source:      source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) createContract(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(
			request.Context(), writer, http.StatusUnauthorized,
			"unauthenticated", "authentication required",
		)
		return
	}
	if r.dependencies.Contracts == nil {
		writeError(
			request.Context(), writer, http.StatusNotImplemented,
			"not_implemented", "contract service is unavailable",
		)
		return
	}
	var body CreateContractRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.Contracts.CreateContract(
		request.Context(),
		clientresources.CreateContractCommand{
			Principal: principal,
			Target:    targetFor(principal),
			DisplayID: body.DisplayID,
			Name:      body.Name,
			StartsOn:  body.StartsOn,
			EndsOn:    body.EndsOn,
			ActorID:   principal.ID,
			Source:    source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) createLocation(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(
			request.Context(), writer, http.StatusUnauthorized,
			"unauthenticated", "authentication required",
		)
		return
	}
	if r.dependencies.Locations == nil {
		writeError(
			request.Context(), writer, http.StatusNotImplemented,
			"not_implemented", "location service is unavailable",
		)
		return
	}
	var body CreateLocationRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.Locations.CreateLocation(
		request.Context(),
		clientresources.CreateLocationCommand{
			Principal: principal,
			Target:    targetFor(principal),
			DisplayID: body.DisplayID,
			Name:      body.Name,
			ActorID:   principal.ID,
			Source:    source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}
