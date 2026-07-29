package airuntime

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const BundleSchemaVersion = 1

type CatalogBundle struct {
	SchemaVersion int           `json:"schema_version" yaml:"schema_version"`
	Kind          string        `json:"kind" yaml:"kind"`
	Runtime       BundleRuntime `json:"runtime" yaml:"runtime"`
}

type BundleRuntime struct {
	CLIs              []CLIDefinition   `json:"clis" yaml:"clis"`
	Models            []ModelDefinition `json:"models" yaml:"models"`
	Profiles          []RuntimeProfile  `json:"profiles" yaml:"profiles"`
	DefaultProfileKey string            `json:"default_profile_key,omitempty" yaml:"default_profile_key,omitempty"`
}

type ImportPreview struct {
	Revision        int64          `json:"catalog_revision"`
	BundleDigest    string         `json:"bundle_digest"`
	Mode            string         `json:"mode"`
	Changes         []ImportChange `json:"changes"`
	ValidationToken string         `json:"validation_token"`
}

type ImportChange struct {
	Entity string `json:"entity"`
	Key    string `json:"key"`
	Action string `json:"action"`
}

type importToken struct {
	Org      string `json:"org"`
	Digest   string `json:"digest"`
	Mode     string `json:"mode"`
	Revision int64  `json:"revision"`
	Expires  int64  `json:"expires"`
}

func newTokenKey() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("airuntime: cannot initialize validation-token key: " + err.Error())
	}
	return key
}

func (s *Service) Export(ctx context.Context, orgID, format string) ([]byte, error) {
	catalog, err := s.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return nil, err
	}
	bundle := bundleFromCatalog(catalog)
	payload, err := exportPayload(bundle)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "json":
		return json.MarshalIndent(payload, "", "  ")
	case "yaml", "yml":
		return yaml.Marshal(payload)
	default:
		return nil, fmt.Errorf("unsupported export format %q", format)
	}
}

func exportPayload(bundle CatalogBundle) (map[string]any, error) {
	raw, err := json.Marshal(bundle)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	var strip func(any)
	strip = func(value any) {
		switch x := value.(type) {
		case map[string]any:
			for _, key := range []string{"id", "created_at", "updated_at", "version", "system"} {
				delete(x, key)
			}
			for _, child := range x {
				strip(child)
			}
		case []any:
			for _, child := range x {
				strip(child)
			}
		}
	}
	strip(payload)
	return payload, nil
}

func (s *Service) PreviewImport(ctx context.Context, orgID, mode string, data []byte) (ImportPreview, error) {
	mode = normalizeImportMode(mode)
	if mode == "" {
		return ImportPreview{}, importError("mode", "mode must be merge, create_only, or replace")
	}
	bundle, digest, err := decodeBundle(data)
	if err != nil {
		return ImportPreview{}, err
	}
	current, err := s.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return ImportPreview{}, err
	}
	next, changes, err := s.buildImportedCatalog(current, bundle, mode)
	if err != nil {
		return ImportPreview{}, err
	}
	if err := validateCatalog(next); err != nil {
		return ImportPreview{}, err
	}
	token := importToken{Org: orgID, Digest: digest, Mode: mode, Revision: current.Revision, Expires: s.now().Add(10 * time.Minute).Unix()}
	signed, err := s.signImportToken(token)
	if err != nil {
		return ImportPreview{}, err
	}
	return ImportPreview{Revision: current.Revision, BundleDigest: digest, Mode: mode, Changes: changes, ValidationToken: signed}, nil
}

func (s *Service) ApplyImport(ctx context.Context, orgID, actor, mode string, data []byte, validationToken string) (Catalog, error) {
	mode = normalizeImportMode(mode)
	bundle, digest, err := decodeBundle(data)
	if err != nil {
		return Catalog{}, err
	}
	token, err := s.verifyImportToken(validationToken)
	if err != nil || token.Org != orgID || token.Digest != digest || token.Mode != mode || token.Expires < s.now().Unix() {
		return Catalog{}, importError("validation_token", "validation token is invalid, expired, or bound to different input")
	}
	current, err := s.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return Catalog{}, err
	}
	if current.Revision != token.Revision {
		return Catalog{}, &Error{Reason: ReasonRevisionConflict, Message: "catalog revision changed after import preview", Details: map[string]any{"expected_revision": token.Revision, "actual_revision": current.Revision}}
	}
	next, _, err := s.buildImportedCatalog(current, bundle, mode)
	if err != nil {
		return Catalog{}, err
	}
	if err := validateCatalog(next); err != nil {
		return Catalog{}, err
	}
	audit := s.audit(orgID, actor, "catalog", orgID, "imported", current, next)
	revision, err := s.repo.ApplyCatalog(ctx, next, current.Revision, audit)
	if err != nil {
		return Catalog{}, err
	}
	next.Revision = revision
	return next, nil
}

func bundleFromCatalog(catalog Catalog) CatalogBundle {
	b := CatalogBundle{SchemaVersion: BundleSchemaVersion, Kind: "agent-center-ai-runtime"}
	b.Runtime.CLIs = append([]CLIDefinition(nil), catalog.CLIs...)
	b.Runtime.Models = append([]ModelDefinition(nil), catalog.Models...)
	b.Runtime.Profiles = append([]RuntimeProfile(nil), catalog.Profiles...)
	for i := range b.Runtime.CLIs {
		b.Runtime.CLIs[i].ID, b.Runtime.CLIs[i].OrgID = "", ""
		b.Runtime.CLIs[i].CreatedAt, b.Runtime.CLIs[i].UpdatedAt = time.Time{}, time.Time{}
		b.Runtime.CLIs[i].ParameterSchema = mustRedactedJSON(b.Runtime.CLIs[i].ParameterSchema)
	}
	for i := range b.Runtime.Models {
		b.Runtime.Models[i].ID, b.Runtime.Models[i].OrgID = "", ""
		b.Runtime.Models[i].CreatedAt, b.Runtime.Models[i].UpdatedAt = time.Time{}, time.Time{}
		b.Runtime.Models[i].DefaultParameters = RedactValue(b.Runtime.Models[i].DefaultParameters).(map[string]any)
	}
	for i := range b.Runtime.Profiles {
		if b.Runtime.Profiles[i].ID == catalog.DefaultProfileID {
			b.Runtime.DefaultProfileKey = b.Runtime.Profiles[i].Key
		}
		b.Runtime.Profiles[i].ID, b.Runtime.Profiles[i].OrgID, b.Runtime.Profiles[i].Version = "", "", 0
		b.Runtime.Profiles[i].CreatedAt, b.Runtime.Profiles[i].UpdatedAt = time.Time{}, time.Time{}
		b.Runtime.Profiles[i].Parameters = RedactValue(b.Runtime.Profiles[i].Parameters).(map[string]any)
	}
	sort.Slice(b.Runtime.CLIs, func(i, j int) bool { return b.Runtime.CLIs[i].Key < b.Runtime.CLIs[j].Key })
	sort.Slice(b.Runtime.Models, func(i, j int) bool { return b.Runtime.Models[i].Key < b.Runtime.Models[j].Key })
	sort.Slice(b.Runtime.Profiles, func(i, j int) bool { return b.Runtime.Profiles[i].Key < b.Runtime.Profiles[j].Key })
	return b
}

func mustRedactedJSON(raw json.RawMessage) json.RawMessage {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return append(json.RawMessage(nil), raw...)
	}
	out, _ := json.Marshal(RedactValue(value))
	return out
}

func decodeBundle(data []byte) (CatalogBundle, string, error) {
	var bundle CatalogBundle
	var generic any
	if err := yaml.Unmarshal(data, &generic); err != nil {
		return bundle, "", importError("bundle", err.Error())
	}
	normalized, err := json.Marshal(generic)
	if err != nil {
		return bundle, "", importError("bundle", err.Error())
	}
	decoder := json.NewDecoder(bytes.NewReader(normalized))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bundle); err != nil {
		return bundle, "", importError("bundle", err.Error())
	}
	if bundle.SchemaVersion != BundleSchemaVersion {
		return bundle, "", &Error{Reason: ReasonImportUnsupported, Message: "AI Runtime bundle schema version is unsupported", Details: map[string]any{"schema_version": bundle.SchemaVersion, "supported": BundleSchemaVersion}}
	}
	if bundle.Kind != "agent-center-ai-runtime" {
		return bundle, "", importError("kind", "kind must be agent-center-ai-runtime")
	}
	canonical, err := CanonicalJSON(bundle)
	if err != nil {
		return bundle, "", err
	}
	return bundle, fmt.Sprintf("sha256:%x", sha256.Sum256(canonical)), nil
}

func normalizeImportMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "merge":
		return "merge"
	case "create_only":
		return "create_only"
	case "replace":
		return "replace"
	default:
		return ""
	}
}

func (s *Service) buildImportedCatalog(current Catalog, bundle CatalogBundle, mode string) (Catalog, []ImportChange, error) {
	next := Catalog{OrgID: current.OrgID, Revision: current.Revision, DefaultProfileID: current.DefaultProfileID}
	next.CLIs, next.Models, next.Profiles = cloneCLIs(current.CLIs), cloneModels(current.Models), cloneProfiles(current.Profiles)
	changes := []ImportChange{}
	applyCLI := func(in CLIDefinition) error {
		if err := validateKey("cli key", in.Key); err != nil {
			return err
		}
		index := cliIndex(next.CLIs, in.Key)
		if index >= 0 && mode == "create_only" {
			changes = append(changes, ImportChange{"cli", in.Key, "unchanged"})
			return nil
		}
		now := s.now()
		if index >= 0 {
			in.ID, in.OrgID, in.System, in.CreatedAt, in.UpdatedAt = next.CLIs[index].ID, current.OrgID, next.CLIs[index].System, next.CLIs[index].CreatedAt, now
			next.CLIs[index] = in
			changes = append(changes, ImportChange{"cli", in.Key, "update"})
		} else {
			in.ID, in.OrgID, in.CreatedAt, in.UpdatedAt = s.id(), current.OrgID, now, now
			next.CLIs = append(next.CLIs, in)
			changes = append(changes, ImportChange{"cli", in.Key, "create"})
		}
		return validateSchema(in.ParameterSchema)
	}
	for _, in := range bundle.Runtime.CLIs {
		if err := applyCLI(in); err != nil {
			return Catalog{}, nil, importError("cli."+in.Key, err.Error())
		}
	}
	for _, in := range bundle.Runtime.Models {
		if err := validateKey("model key", in.Key); err != nil {
			return Catalog{}, nil, err
		}
		index, now := modelIndex(next.Models, in.Key), s.now()
		if index >= 0 && mode == "create_only" {
			changes = append(changes, ImportChange{"model", in.Key, "unchanged"})
			continue
		}
		if index >= 0 {
			in.ID, in.OrgID, in.CreatedAt, in.UpdatedAt = next.Models[index].ID, current.OrgID, next.Models[index].CreatedAt, now
			next.Models[index] = in
			changes = append(changes, ImportChange{"model", in.Key, "update"})
		} else {
			in.ID, in.OrgID, in.CreatedAt, in.UpdatedAt = s.id(), current.OrgID, now, now
			next.Models = append(next.Models, in)
			changes = append(changes, ImportChange{"model", in.Key, "create"})
		}
	}
	for _, in := range bundle.Runtime.Profiles {
		if err := validateKey("profile key", in.Key); err != nil {
			return Catalog{}, nil, err
		}
		index, now := profileIndex(next.Profiles, in.Key), s.now()
		if index >= 0 && mode == "create_only" {
			changes = append(changes, ImportChange{"profile", in.Key, "unchanged"})
			continue
		}
		if index >= 0 {
			old := next.Profiles[index]
			in.ID, in.OrgID, in.Version, in.CreatedAt, in.UpdatedAt = old.ID, current.OrgID, old.Version+1, old.CreatedAt, now
			next.Profiles[index] = in
			changes = append(changes, ImportChange{"profile", in.Key, "update"})
		} else {
			in.ID, in.OrgID, in.Version, in.CreatedAt, in.UpdatedAt = s.id(), current.OrgID, 1, now, now
			next.Profiles = append(next.Profiles, in)
			changes = append(changes, ImportChange{"profile", in.Key, "create"})
		}
	}
	if bundle.Runtime.DefaultProfileKey != "" {
		index := profileIndex(next.Profiles, bundle.Runtime.DefaultProfileKey)
		if index < 0 {
			return Catalog{}, nil, importError("default_profile_key", "default profile is missing")
		}
		next.DefaultProfileID = next.Profiles[index].ID
	} else if mode == "replace" {
		next.DefaultProfileID = ""
	}
	if mode == "replace" {
		// Resolve the replacement default before disabling missing profiles.
		// Otherwise the previous default is incorrectly kept enabled even
		// though it is absent from the replacement bundle.
		disableMissing(bundle, &next, &changes)
	}
	return next, changes, nil
}

func validateCatalog(c Catalog) error {
	for _, cli := range c.CLIs {
		if err := validateSchema(cli.ParameterSchema); err != nil {
			return err
		}
	}
	for _, model := range c.Models {
		if err := validateModel(c, model); err != nil {
			return err
		}
	}
	for _, profile := range c.Profiles {
		if profile.Enabled {
			if err := validateProfile(c, profile); err != nil {
				return err
			}
		}
	}
	if c.DefaultProfileID != "" {
		p := findProfile(c.Profiles, c.DefaultProfileID)
		if p == nil || !p.Enabled {
			return importError("default_profile_key", "default profile must exist and be enabled")
		}
	}
	return nil
}

func (s *Service) signImportToken(token importToken) (string, error) {
	body, err := json.Marshal(token)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, s.tokenKey)
	_, _ = mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func (s *Service) verifyImportToken(raw string) (importToken, error) {
	var token importToken
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return token, errors.New("invalid token")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return token, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return token, err
	}
	mac := hmac.New(sha256.New, s.tokenKey)
	_, _ = mac.Write(body)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return token, errors.New("invalid signature")
	}
	return token, json.Unmarshal(body, &token)
}

func importError(field, message string) error {
	return &Error{Reason: ReasonImportInvalid, Message: "AI Runtime import validation failed", Details: map[string]any{"field": field, "error": message}}
}

func cloneCLIs(v []CLIDefinition) []CLIDefinition {
	raw, _ := json.Marshal(v)
	var out []CLIDefinition
	_ = json.Unmarshal(raw, &out)
	return out
}
func cloneModels(v []ModelDefinition) []ModelDefinition {
	raw, _ := json.Marshal(v)
	var out []ModelDefinition
	_ = json.Unmarshal(raw, &out)
	return out
}
func cloneProfiles(v []RuntimeProfile) []RuntimeProfile {
	raw, _ := json.Marshal(v)
	var out []RuntimeProfile
	_ = json.Unmarshal(raw, &out)
	return out
}
func cliIndex(v []CLIDefinition, key string) int {
	for i := range v {
		if v[i].Key == key {
			return i
		}
	}
	return -1
}
func modelIndex(v []ModelDefinition, key string) int {
	for i := range v {
		if v[i].Key == key {
			return i
		}
	}
	return -1
}
func profileIndex(v []RuntimeProfile, key string) int {
	for i := range v {
		if v[i].Key == key {
			return i
		}
	}
	return -1
}

func disableMissing(bundle CatalogBundle, next *Catalog, changes *[]ImportChange) {
	cliKeys, modelKeys, profileKeys := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, x := range bundle.Runtime.CLIs {
		cliKeys[x.Key] = true
	}
	for _, x := range bundle.Runtime.Models {
		modelKeys[x.Key] = true
	}
	for _, x := range bundle.Runtime.Profiles {
		profileKeys[x.Key] = true
	}
	for i := range next.CLIs {
		if !next.CLIs[i].System && !cliKeys[next.CLIs[i].Key] && next.CLIs[i].Enabled {
			next.CLIs[i].Enabled = false
			*changes = append(*changes, ImportChange{"cli", next.CLIs[i].Key, "disable"})
		}
	}
	for i := range next.Models {
		if !modelKeys[next.Models[i].Key] && next.Models[i].Enabled {
			next.Models[i].Enabled = false
			*changes = append(*changes, ImportChange{"model", next.Models[i].Key, "disable"})
		}
	}
	for i := range next.Profiles {
		if !profileKeys[next.Profiles[i].Key] && next.Profiles[i].Enabled && next.Profiles[i].ID != next.DefaultProfileID {
			next.Profiles[i].Enabled = false
			*changes = append(*changes, ImportChange{"profile", next.Profiles[i].Key, "disable"})
		}
	}
}
