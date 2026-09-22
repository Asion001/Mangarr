package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/series"
)

// A series' sources can be reordered in one request and a wrong match
// swapped in place, keeping its position.
func TestReorderAndReplaceSources(t *testing.T) {
	srv, a := newServer(t, true)
	var rf model.RootFolder
	if code := doJSON(t, http.MethodPost, srv.URL+"/api/v1/rootfolders", `{"path":`+jsonString(t.TempDir())+`,"language":"en"}`, &rf); code != 200 {
		t.Fatalf("root folder: %d", code)
	}
	ser, err := a.Series.Add(context.Background(), series.AddRequest{Title: "Two Sources", RootFolderID: rf.ID, Monitor: model.MonitorNone, NoRefresh: true,
		Sources: []series.SourceLink{{ModuleID: 1, SourceID: "A", URL: "/a", SourceName: "Source A", Lang: "en"}, {ModuleID: 1, SourceID: "B", URL: "/b", SourceName: "Source B", Lang: "en"}}})
	if err != nil {
		t.Fatal(err)
	}
	type view struct {
		SourcePriorityMode string `json:"sourcePriorityMode"`
		Sources            []struct {
			ID       int64  `json:"id"`
			SourceID string `json:"sourceId"`
			MangaURL string `json:"mangaUrl"`
			Priority int    `json:"priority"`
			Chapters *int   `json:"chapters"`
			Files    *int   `json:"files"`
		} `json:"sources"`
	}
	get := func() view {
		var v view
		doJSON(t, http.MethodGet, srv.URL+"/api/v1/series/"+itoa(ser.ID), "", &v)
		return v
	}
	v := get()
	if len(v.Sources) != 2 || v.Sources[0].SourceID != "A" || v.Sources[0].Chapters == nil || v.Sources[0].Files == nil {
		t.Fatalf("sources with counts: %+v", v.Sources)
	}
	a1, b1 := v.Sources[0].ID, v.Sources[1].ID

	// one request, B first; the series switches to its own order
	if code := doJSON(t, http.MethodPut, srv.URL+"/api/v1/series/"+itoa(ser.ID)+"/sources/order", `{"linkIds":[`+itoa(b1)+`,`+itoa(a1)+`]}`, nil); code != 204 && code != 200 {
		t.Fatalf("reorder: %d", code)
	}
	v = get()
	if v.SourcePriorityMode != "custom" || v.Sources[0].SourceID != "B" {
		t.Fatalf("after reorder: %+v", v)
	}
	// every link, once
	if code := doJSON(t, http.MethodPut, srv.URL+"/api/v1/series/"+itoa(ser.ID)+"/sources/order", `{"linkIds":[`+itoa(b1)+`]}`, nil); code != 400 {
		t.Fatalf("partial order: want 400, got %d", code)
	}

	// swap A for another entry: same place, A gone
	body := `{"moduleId":1,"sourceId":"A","url":"/a-main","sourceName":"Source A","lang":"en"}`
	if code := doJSON(t, http.MethodPost, srv.URL+"/api/v1/series/"+itoa(ser.ID)+"/sources/"+itoa(a1)+"/replace", body, nil); code != 200 {
		t.Fatalf("replace: %d", code)
	}
	v = get()
	if len(v.Sources) != 2 || v.Sources[1].MangaURL != "/a-main" || v.Sources[1].Priority != 1 || v.Sources[1].ID == a1 {
		t.Fatalf("after replace: %+v", v.Sources)
	}
	// replacing with a link the series already has is a conflict
	body = `{"moduleId":1,"sourceId":"B","url":"/b","sourceName":"Source B","lang":"en"}`
	if code := doJSON(t, http.MethodPost, srv.URL+"/api/v1/series/"+itoa(ser.ID)+"/sources/"+itoa(v.Sources[1].ID)+"/replace", body, nil); code != 409 {
		t.Fatalf("duplicate replace: want 409, got %d", code)
	}
	if len(get().Sources) != 2 {
		t.Fatal("a failed replace must leave the old link in place")
	}
}

func jsonString(s string) string { b, _ := json.Marshal(s); return string(b) }
