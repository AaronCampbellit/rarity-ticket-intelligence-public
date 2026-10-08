package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/setup"
)

type SetupActions interface {
	Status(context.Context) (bool, bool, error)
	CenterStatus(context.Context, authorization.Principal) (setup.CenterStatus, error)
	UpdateCenterConfiguration(
		context.Context,
		setup.UpdateCenterConfigurationCommand,
	) (setup.CenterStatus, error)
	VerifyObjectStorage(
		context.Context,
		setup.VerifyObjectStorageCommand,
	) (setup.CenterStatus, error)
	AcceptBackupEvidence(
		context.Context,
		setup.AcceptBackupEvidenceCommand,
	) (setup.CenterStatus, error)
	Bootstrap(context.Context, string, setup.Configuration) error
}

type updateSetupCenterRequest struct {
	ExpectedVersion int64          `json:"expected_version"`
	Intake          map[string]any `json:"intake"`
	ObjectStorage   map[string]any `json:"object_storage"`
	Backups         map[string]any `json:"backups"`
	Reason          string         `json:"reason"`
}

func (r *Router) registerSetupRoutes() {
	r.mux.HandleFunc("GET /api/v1/setup/status", r.setupStatus)
	r.mux.HandleFunc("GET /api/v1/setup/center", r.setupCenter)
	r.mux.HandleFunc(
		"PUT /api/v1/setup/center/configuration",
		r.updateSetupCenterConfiguration,
	)
	r.mux.HandleFunc(
		"POST /api/v1/setup/center/object-storage/verify",
		r.verifySetupObjectStorage,
	)
	r.mux.HandleFunc(
		"POST /api/v1/setup/center/backups/evidence",
		r.acceptSetupBackupEvidence,
	)
	r.mux.HandleFunc("POST /api/v1/setup/bootstrap", r.bootstrapInstallation)
}

func (r *Router) acceptSetupBackupEvidence(
	writer http.ResponseWriter,
	request *http.Request,
) {
	if r.dependencies.Setup == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "setup is unavailable")
		return
	}
	var body struct {
		ExpectedVersion int64                       `json:"expected_version"`
		Payload         setup.BackupEvidencePayload `json:"payload"`
	}
	if !decodeRequest(writer, request, &body) {
		return
	}
	signature := strings.TrimSpace(strings.TrimPrefix(
		request.Header.Get("Authorization"), "BackupEvidence ",
	))
	if signature == "" || signature == request.Header.Get("Authorization") {
		writeError(request.Context(), writer, http.StatusUnauthorized, "invalid_backup_evidence", "backup evidence is invalid")
		return
	}
	status, err := r.dependencies.Setup.AcceptBackupEvidence(
		request.Context(),
		setup.AcceptBackupEvidenceCommand{
			Payload: body.Payload, Signature: signature,
			ExpectedVersion: body.ExpectedVersion, Source: source(request),
		},
	)
	if err != nil {
		if errors.Is(err, setup.ErrInvalidBackupEvidence) {
			writeError(request.Context(), writer, http.StatusUnauthorized, "invalid_backup_evidence", "backup evidence is invalid")
			return
		}
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, status.ConfigurationVersion)
	writeJSON(writer, http.StatusOK, status)
}

func (r *Router) verifySetupObjectStorage(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Setup == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "setup is unavailable")
		return
	}
	var body struct {
		ExpectedVersion int64  `json:"expected_version"`
		Reason          string `json:"reason"`
	}
	if !decodeRequest(writer, request, &body) {
		return
	}
	status, err := r.dependencies.Setup.VerifyObjectStorage(
		request.Context(),
		setup.VerifyObjectStorageCommand{
			Principal: principal, ExpectedVersion: body.ExpectedVersion,
			Reason: body.Reason, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, status.ConfigurationVersion)
	writeJSON(writer, http.StatusOK, status)
}

func (r *Router) updateSetupCenterConfiguration(
	writer http.ResponseWriter,
	request *http.Request,
) {
	_, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Setup == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "setup is unavailable")
		return
	}
	writeError(
		request.Context(), writer, http.StatusGone,
		"setup_reference_editor_retired",
		"use the guided Setup Center workflows",
	)
}

func (r *Router) setupCenter(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Setup == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "setup is unavailable")
		return
	}
	status, err := r.dependencies.Setup.CenterStatus(request.Context(), principal)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, status)
}

func (r *Router) setupStatus(writer http.ResponseWriter, request *http.Request) {
	if r.dependencies.Setup == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "setup is unavailable")
		return
	}
	completed, entraAvailable, err := r.dependencies.Setup.Status(request.Context())
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{
		"completed": completed, "bootstrap_available": !completed,
		"entra_available": entraAvailable,
	})
}

func (r *Router) bootstrapInstallation(writer http.ResponseWriter, request *http.Request) {
	if r.dependencies.Setup == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "setup is unavailable")
		return
	}
	if request.TLS == nil &&
		!strings.EqualFold(strings.TrimSpace(request.Header.Get("X-Forwarded-Proto")), "https") {
		writeError(request.Context(), writer, http.StatusUpgradeRequired, "https_required", "setup requires HTTPS")
		return
	}
	token := strings.TrimSpace(strings.TrimPrefix(
		request.Header.Get("Authorization"), "Bootstrap ",
	))
	if token == "" || token == request.Header.Get("Authorization") {
		writeError(request.Context(), writer, http.StatusUnauthorized, "invalid_bootstrap_token", "bootstrap token is invalid")
		return
	}
	var body setup.Configuration
	if !decodeRequest(writer, request, &body) {
		return
	}
	if err := r.dependencies.Setup.Bootstrap(request.Context(), token, body); err != nil {
		if errors.Is(err, setup.ErrBootstrapClosed) {
			writeError(request.Context(), writer, http.StatusGone, "bootstrap_closed", "bootstrap is unavailable")
			return
		}
		if errors.Is(err, setup.ErrInvalidBootstrap) {
			writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "setup configuration is invalid")
			return
		}
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]bool{"completed": true})
}
