package api

import (
	"errors"
	"net/http"

	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/team"
	teamservice "github.com/oopslink/agent-center/internal/team/service"
)

type teamRAMRoleMappingRequest struct {
	RAMRoleIDs      []string `json:"ram_role_ids"`
	ExpectedVersion int      `json:"expected_version"`
}

func (s *Server) getTeamRAMRoleMappingHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, _, orgID, ok := teamGuardMember(w, r, d)
	if !ok {
		return
	}
	if _, err := getTeamInOrg(r, d, orgID, r.PathValue("id")); err != nil {
		mapTeamWebError(w, err)
		return
	}
	mapping, err := d.TeamService.GetRAMRoleMapping(r.Context(), team.TeamID(r.PathValue("id")), r.PathValue("role"))
	if err != nil {
		mapTeamRAMRoleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mapping)
}

func (s *Server) previewTeamRAMRoleMappingHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, _, orgID, ok := teamGuardMember(w, r, d)
	if !ok {
		return
	}
	if _, err := getTeamInOrg(r, d, orgID, r.PathValue("id")); err != nil {
		mapTeamWebError(w, err)
		return
	}
	var req teamRAMRoleMappingRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	impact, err := d.TeamService.PreviewRAMRoleMapping(r.Context(), team.TeamID(r.PathValue("id")), r.PathValue("role"), req.RAMRoleIDs)
	if err != nil {
		mapTeamRAMRoleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, impact)
}

func (s *Server) putTeamRAMRoleMappingHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	caller, _, orgID, ok := teamGuardMember(w, r, d)
	if !ok {
		return
	}
	if _, err := getTeamInOrg(r, d, orgID, r.PathValue("id")); err != nil {
		mapTeamWebError(w, err)
		return
	}
	var req teamRAMRoleMappingRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	mapping, err := d.TeamService.ReplaceRAMRoleMapping(r.Context(), team.TeamID(r.PathValue("id")), r.PathValue("role"), teamservice.ReplaceRAMRoleMappingInput{
		ActorRef: string(authz.UserSubject(caller.ID())), RAMRoleIDs: req.RAMRoleIDs, ExpectedVersion: req.ExpectedVersion,
	})
	if err != nil {
		mapTeamRAMRoleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mapping)
}

func mapTeamRAMRoleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, teamservice.ErrRAMRoleMappingConflict):
		writeError(w, http.StatusConflict, "version_conflict", err.Error())
	case errors.Is(err, teamservice.ErrRAMRoleNotFound):
		writeError(w, http.StatusUnprocessableEntity, "invalid_ram_role", err.Error())
	case errors.Is(err, team.ErrRoleNotDeclared):
		writeError(w, http.StatusNotFound, "team_role_not_found", err.Error())
	default:
		mapTeamWebError(w, err)
	}
}
