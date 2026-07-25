package goai

import (
	"net/http"
	"time"
)

// ApplyModelCatalogRevalidationHeaders attaches stored validators to a remote
// model-catalog request. ETag is echoed verbatim, including quotes.
func ApplyModelCatalogRevalidationHeaders(req *http.Request, entry *ModelsStoreEntry) {
	if req == nil || entry == nil {
		return
	}
	if entry.ETag != "" {
		req.Header.Set("If-None-Match", entry.ETag)
	}
	if entry.LastModified > 0 {
		req.Header.Set("If-Modified-Since", time.UnixMilli(entry.LastModified).UTC().Format(http.TimeFormat))
	}
}

// UpdateModelCatalogValidators records ETag/Last-Modified validators from a
// successful remote model-catalog response into an existing entry.
func UpdateModelCatalogValidators(entry *ModelsStoreEntry, resp *http.Response) {
	if entry == nil || resp == nil {
		return
	}
	if etag := resp.Header.Get("ETag"); etag != "" {
		entry.ETag = etag
	}
	if lastModified := resp.Header.Get("Last-Modified"); lastModified != "" {
		if when, err := http.ParseTime(lastModified); err == nil {
			entry.LastModified = when.UnixMilli()
		}
	}
}
