package komgaapi

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Asion001/mangarr/internal/model"
)

// userDTO is Komga's UserDto. KMReader requires id, email and roles.
type userDTO struct {
	ID                 string   `json:"id"`
	Email              string   `json:"email"`
	Roles              []string `json:"roles"`
	SharedAllLibraries bool     `json:"sharedAllLibraries"`
	SharedLibrariesIDs []string `json:"sharedLibrariesIds"`
	LabelsAllow        []string `json:"labelsAllow"`
	LabelsExclude      []string `json:"labelsExclude"`
}

// apiKeyDTO is Komga's ApiKeyDto (KMReader creates one after a password login).
type apiKeyDTO struct {
	ID               string `json:"id"`
	UserID           string `json:"userId"`
	Key              string `json:"key"`
	Comment          string `json:"comment"`
	CreatedDate      string `json:"createdDate"`
	LastModifiedDate string `json:"lastModifiedDate"`
}

type authHandlers struct{ s *Service }

// account is the caller's user id and name.
func account(r *http.Request) (int64, string) {
	p := PrincipalFrom(r.Context())
	if p.User == nil || p.User.UserID == 0 {
		return 0, "mangarr"
	}
	return p.User.UserID, p.User.Username
}

func (h *authHandlers) me(w http.ResponseWriter, r *http.Request) {
	id, name := account(r)
	if id == 0 {
		id = 1
	}
	// no ADMIN role: apps then skip admin-only endpoints
	writeJSON(w, http.StatusOK, userDTO{ID: strconv.FormatInt(id, 10), Email: name, Roles: []string{"USER", "FILE_DOWNLOAD", "PAGE_STREAMING"},
		SharedAllLibraries: true, SharedLibrariesIDs: []string{}, LabelsAllow: []string{}, LabelsExclude: []string{}})
}

func keyDTO(rk model.ReadingKey, key string) apiKeyDTO {
	if key == "" {
		key = rk.Prefix + "…"
	}
	modified := rk.CreatedAt
	if rk.LastUsedAt != nil {
		modified = *rk.LastUsedAt
	}
	return apiKeyDTO{ID: strconv.FormatInt(rk.ID, 10), UserID: strconv.FormatInt(max(rk.UserID, 1), 10), Key: key, Comment: rk.Comment,
		CreatedDate: komgaTime(rk.CreatedAt), LastModifiedDate: komgaTime(modified)}
}

func (h *authHandlers) listKeys(w http.ResponseWriter, r *http.Request) {
	uid, _ := account(r)
	var list []model.ReadingKey
	if err := h.s.deps.DB.NewSelect().Model(&list).Where("COALESCE(user_id, 0) = ?", uid).Order("id").Scan(r.Context()); err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]apiKeyDTO, 0, len(list))
	for _, rk := range list {
		out = append(out, keyDTO(rk, ""))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *authHandlers) createKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Comment string `json:"comment"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	p := PrincipalFrom(r.Context())
	comment := body.Comment
	if comment == "" {
		comment = p.Client
	}
	uid, _ := account(r)
	key, rk, err := h.s.CreateKey(r.Context(), uid, comment, p.Client)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, keyDTO(*rk, key))
}

func (h *authHandlers) deleteKey(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	uid, _ := account(r)
	if _, err := h.s.deps.DB.NewDelete().Model((*model.ReadingKey)(nil)).Where("id = ? AND COALESCE(user_id, 0) = ?", id, uid).Exec(r.Context()); err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	h.s.InvalidateKeys()
	w.WriteHeader(http.StatusNoContent)
}

func (h *authHandlers) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(0, 0)})
	w.Header().Del("X-Auth-Token")
	w.WriteHeader(http.StatusNoContent)
}

// libraryDTO is Komga's LibraryDto (clients need id, name and root).
type libraryDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Root        string `json:"root"`
	Unavailable bool   `json:"unavailable"`
}

type libraryHandlers struct{ s *Service }

func libraryFrom(rf model.RootFolder) libraryDTO {
	name := filepath.Base(rf.Path)
	if rf.Language != "" {
		name += " (" + rf.Language + ")"
	}
	return libraryDTO{ID: strconv.FormatInt(rf.ID, 10), Name: name, Root: rf.Path}
}

func (h *libraryHandlers) list(w http.ResponseWriter, r *http.Request) {
	var roots []model.RootFolder
	if err := h.s.deps.DB.NewSelect().Model(&roots).Order("id").Scan(r.Context()); err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]libraryDTO, 0, len(roots))
	limit := PrincipalFrom(r.Context()).User.Scope.RootFolders
	for _, rf := range roots {
		if len(limit) == 0 || slices.Contains(limit, rf.ID) {
			out = append(out, libraryFrom(rf))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *libraryHandlers) get(w http.ResponseWriter, r *http.Request) {
	var rf model.RootFolder
	if err := h.s.deps.DB.NewSelect().Model(&rf).Where("id = ?", chi.URLParam(r, "id")).Scan(r.Context()); err != nil {
		writeError(w, r, http.StatusNotFound, "library not found")
		return
	}
	writeJSON(w, http.StatusOK, libraryFrom(rf))
}
