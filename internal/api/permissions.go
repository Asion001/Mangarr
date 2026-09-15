package api

import (
	"net/http"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Asion001/mangarr/internal/access"
)

// Permission requirements beyond the ones in internal/access.
const (
	permPublic   = "public"   // no login
	permSignedIn = "signedin" // any signed-in user
)

// operationPermissions lists the operations people without the admin
// permission may use, with the permissions that allow each (any of them).
// Operations missing here need admin: a new endpoint is admin-only until
// it's added (testdata/permissions.txt keeps the list reviewed).
var operationPermissions = map[string][]string{
	// signing in, and one's own account
	"auth-status":               {permPublic},
	"auth-login":                {permPublic},
	"auth-setup":                {permPublic},
	"invite-get":                {permPublic},
	"invite-redeem":             {permPublic},
	"auth-logout":               {permSignedIn},
	"auth-password":             {permSignedIn},
	"me-sessions":               {permSignedIn},
	"me-sessions-revoke":        {permSignedIn},
	"me-sessions-revoke-others": {permSignedIn},

	// reading the library (limited to the series the group sees)
	"series-list":     {permSignedIn},
	"series-get":      {permSignedIn},
	"series-chapters": {permSignedIn},
	"series-cover":    {permSignedIn},
	"reading-shelf":   {permSignedIn},
	"tags-list":       {permSignedIn},

	// reading apps (their own devices)
	"reading-status":      {access.Apps},
	"reading-keys":        {access.Apps},
	"reading-keys-create": {access.Apps},
	"reading-keys-delete": {access.Apps},

	// finding series (to add, or to request)
	"series-lookup":     {access.LibraryManage, access.RequestsCreate},
	"series-lookup-get": {access.LibraryManage, access.RequestsCreate},

	// managing the library
	"series-add":              {access.LibraryManage},
	"series-update":           {access.LibraryManage},
	"series-delete":           {access.LibraryManage},
	"series-editor":           {access.LibraryManage},
	"series-metadata-link":    {access.LibraryManage},
	"series-metadata-refresh": {access.LibraryManage},
	"series-refresh":          {access.LibraryManage},
	"series-rename":           {access.LibraryManage},
	"series-rename-preview":   {access.LibraryManage},
	"series-search":           {access.LibraryManage},
	"series-source-link":      {access.LibraryManage},
	"series-source-unlink":    {access.LibraryManage},
	"series-source-update":    {access.LibraryManage},
	"chapter-decision":        {access.LibraryManage},
	"chapter-restore":         {access.LibraryManage},
	"chapters-monitor":        {access.LibraryManage},
	"queue-list":              {access.LibraryManage},
	"queue-bulk":              {access.LibraryManage},
	"queue-clear":             {access.LibraryManage},
	"queue-pause":             {access.LibraryManage},
	"queue-resume":            {access.LibraryManage},
	"queue-remove":            {access.LibraryManage},
	"queue-retry":             {access.LibraryManage},
	"history-list":            {access.LibraryManage},
	"blocklist-list":          {access.LibraryManage},
	"blocklist-delete":        {access.LibraryManage},
	"wanted-missing":          {access.LibraryManage},
	"calendar":                {access.LibraryManage},
	"sources-list":            {access.LibraryManage},
	"sources-search":          {access.LibraryManage},
	"sources-quick-search":    {access.LibraryManage},
	"sources-browse":          {access.LibraryManage},
	"sources-manga":           {access.LibraryManage},
	"sources-thumbnail":       {access.LibraryManage},
	"catalogs-list":           {access.LibraryManage},
	"rootfolders-list":        {access.LibraryManage},
	"profiles-list":           {access.LibraryManage},
	"tags-create":             {access.LibraryManage},
	"tags-delete":             {access.LibraryManage},
	"modules-asset":           {access.LibraryManage},
	"commands-push":           {access.LibraryManage}, // non-admins: managerCommands only
	"commands-list":           {access.LibraryManage},
	"commands-get":            {access.LibraryManage},
}

// permissionsFor returns what an operation needs (admin when unlisted).
func permissionsFor(op string) []string {
	if p, ok := operationPermissions[op]; ok {
		return p
	}
	return []string{access.Admin}
}

// allowed reports whether p may use an operation that needs any of need.
func allowed(p *access.Principal, need []string) bool {
	for _, n := range need {
		switch {
		case n == permPublic:
			return true
		case n == permSignedIn && p != nil:
			return true
		case p.Can(n):
			return true
		}
	}
	return false
}

// requirePermission enforces operationPermissions.
func (s *Server) requirePermission(ctx huma.Context, next func(huma.Context)) {
	need := permissionsFor(ctx.Operation().OperationID)
	p := access.From(ctx.Context())
	if allowed(p, need) {
		next(ctx)
		return
	}
	if p == nil {
		_ = huma.WriteErr(s.api, ctx, http.StatusUnauthorized, "login or X-Api-Key required")
		return
	}
	_ = huma.WriteErr(s.api, ctx, http.StatusForbidden, "your account can't do this ("+strings.Join(need, " or ")+" needed)")
}

// PermissionTable lists every operation with what it needs, sorted (for
// the golden test and docs).
func PermissionTable(api huma.API) []string {
	var out []string
	for path, item := range api.OpenAPI().Paths {
		for method, op := range map[string]*huma.Operation{"GET": item.Get, "POST": item.Post, "PUT": item.Put, "PATCH": item.Patch, "DELETE": item.Delete} {
			if op == nil {
				continue
			}
			out = append(out, op.OperationID+" "+method+" "+path+" -> "+strings.Join(permissionsFor(op.OperationID), " | "))
		}
	}
	sort.Strings(out)
	return out
}
