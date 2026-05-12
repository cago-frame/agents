package coding

import (
	"testing"

	"github.com/cago-frame/agents/tool/webfetch"
	"github.com/cago-frame/agents/tool/websearch"
)

func TestGeneralPurposeTools_WithSearch_AttachesWebSearch(t *testing.T) {
	mp := &websearch.MockProvider{}
	spec := &websearchSpec{prov: mp}
	tools := generalPurposeTools(".", spec, nil)
	var found bool
	for _, tt := range tools {
		if tt.Name() == "web_search" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("web_search missing from GP tools when WithSearch is set")
	}
}

func TestGeneralPurposeTools_WithFetch_AttachesWebFetch(t *testing.T) {
	spec := &webfetchSpec{opts: []webfetch.Option{}}
	tools := generalPurposeTools(".", nil, spec)
	var found bool
	for _, tt := range tools {
		if tt.Name() == "web_fetch" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("web_fetch missing from GP tools when WithFetch is set")
	}
}
